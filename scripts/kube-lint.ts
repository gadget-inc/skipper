#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import { $, chalk } from "zx";

import { abs, emptyDir, isCI } from "./_utils.ts";

if (isCI) {
  chalk.level = 0;
}

$.cwd = abs();

interface BindingConfig {
  name: string;
  bindings: Record<string, unknown>;
}

const baseBindings = {
  namespace: "lint",
  function_namespaces: ["default"],
  image_tag: "v1.0.0",
};

const configs: BindingConfig[] = [
  {
    name: "minimal",
    bindings: {},
  },
  {
    name: "hpa-enabled",
    bindings: {
      controller_min_replicas: 2,
      controller_max_replicas: 10,
      router_min_replicas: 2,
      router_max_replicas: 10,
    },
  },
  {
    name: "grpc-protocol",
    bindings: {
      controller_protocol: "grpc",
    },
  },
  {
    name: "nodeport",
    bindings: {
      router_node_port: 30080,
    },
  },
  {
    name: "inline-paseto",
    bindings: {
      unsafe_controller_paseto_private_key: "k4.secret.fake-paseto-key-for-linting",
    },
  },
  {
    name: "controller-affinity-override",
    bindings: {
      controller_affinity: {
        podAntiAffinity: {
          requiredDuringSchedulingIgnoredDuringExecution: [
            {
              topologyKey: "kubernetes.io/hostname",
              labelSelector: {
                matchLabels: {
                  "app.kubernetes.io/name": "skipper",
                  "app.kubernetes.io/component": "controller",
                },
              },
            },
          ],
        },
      },
    },
  },
  {
    name: "router-affinity-override",
    bindings: {
      router_affinity: {
        podAntiAffinity: {
          requiredDuringSchedulingIgnoredDuringExecution: [
            {
              topologyKey: "kubernetes.io/hostname",
              labelSelector: {
                matchLabels: {
                  "app.kubernetes.io/name": "skipper",
                  "app.kubernetes.io/component": "router",
                },
              },
            },
          ],
        },
      },
    },
  },
  {
    name: "combined-affinity",
    bindings: {
      controller_affinity: {
        nodeAffinity: {
          requiredDuringSchedulingIgnoredDuringExecution: {
            nodeSelectorTerms: [
              {
                matchExpressions: [
                  {
                    key: "node-type",
                    operator: "In",
                    values: ["compute"],
                  },
                ],
              },
            ],
          },
        },
        podAntiAffinity: {
          preferredDuringSchedulingIgnoredDuringExecution: [
            {
              weight: 100,
              podAffinityTerm: {
                topologyKey: "kubernetes.io/hostname",
                labelSelector: {
                  matchLabels: {
                    "app.kubernetes.io/name": "skipper",
                    "app.kubernetes.io/component": "controller",
                  },
                },
              },
            },
          ],
        },
      },
      router_affinity: {
        nodeAffinity: {
          requiredDuringSchedulingIgnoredDuringExecution: {
            nodeSelectorTerms: [
              {
                matchExpressions: [
                  {
                    key: "node-type",
                    operator: "In",
                    values: ["compute"],
                  },
                ],
              },
            ],
          },
        },
        podAntiAffinity: {
          preferredDuringSchedulingIgnoredDuringExecution: [
            {
              weight: 100,
              podAffinityTerm: {
                topologyKey: "kubernetes.io/hostname",
                labelSelector: {
                  matchLabels: {
                    "app.kubernetes.io/name": "skipper",
                    "app.kubernetes.io/component": "router",
                  },
                },
              },
            },
          ],
        },
      },
    },
  },
  {
    name: "full-features",
    bindings: {
      controller_annotations: { "prometheus.io/scrape": "true" },
      router_annotations: { "prometheus.io/scrape": "true" },
      controller_tolerations: [{ key: "dedicated", operator: "Equal", value: "skipper", effect: "NoSchedule" }],
      router_tolerations: [{ key: "dedicated", operator: "Equal", value: "skipper", effect: "NoSchedule" }],
      controller_node_selector: { "node-pool": "skipper" },
      router_node_selector: { "node-pool": "skipper" },
    },
  },
];

const lintDir = "tmp/kube-lint";
await emptyDir(abs(lintDir));

let hasErrors = false;

for (const config of configs) {
  const configDir = `${lintDir}/${config.name}`;
  await emptyDir(abs(configDir));

  const mergedBindings = { ...baseBindings, ...config.bindings };

  // Render template with krane
  await $({
    verbose: false,
    quiet: true,
  })`krane render --filenames=${abs("template.yaml.erb")} --bindings=${JSON.stringify(mergedBindings)} > ${configDir}/template.yaml 2>/dev/null`;

  // Run yamllint for duplicate key detection (parsable format for compact output)
  const yamllintResult = await $({
    nothrow: true,
    quiet: true,
  })`yamllint -f parsable -c ${abs(".yamllint.yaml")} ${configDir}/template.yaml`;

  if (yamllintResult.exitCode !== 0) {
    // parsable format: file:line:col: [error] message
    const output = (yamllintResult.stdout || yamllintResult.stderr).trim();
    if (output) {
      for (const line of output.split("\n")) {
        const match = line.match(/^(.+?:\d+:\d+): (\[error\]) (.+)$/);
        if (match) {
          console.error(`${chalk.dim(match[1])}: ${chalk.red(match[2])} ${match[3]}`);
        } else {
          console.error(line);
        }
      }
    }
    hasErrors = true;
  }

  // Run kube-linter for Kubernetes validation
  const kubeLinterResult = await $({
    nothrow: true,
    quiet: true,
  })`kube-linter lint --format json ${configDir}/template.yaml`;

  if (kubeLinterResult.exitCode !== 0) {
    // Parse JSON output for compact error display
    try {
      const output = JSON.parse(kubeLinterResult.stdout);
      const basePath = abs().replace(/\/$/, "") + "/";
      for (const report of output.Reports || []) {
        let file = report.Object?.Metadata?.FilePath || configDir + "/template.yaml";
        // Convert absolute paths to relative
        if (file.startsWith(basePath)) {
          file = file.slice(basePath.length);
        }
        const objectName = report.Object?.K8sObject?.Name || "unknown";
        const check = report.Check || "unknown";
        const message = report.Diagnostic?.Message || "unknown error";
        console.error(`${chalk.dim(file)}: ${objectName} [${chalk.yellow(check)}]: ${chalk.red(message)}`);
      }
    } catch {
      // Fallback if JSON parsing fails
      console.error(kubeLinterResult.stdout || kubeLinterResult.stderr);
    }
    hasErrors = true;
  }
}

if (hasErrors) {
  process.exit(1);
}

console.log(chalk.green(`✓ ${configs.length} template configurations linted successfully`));
