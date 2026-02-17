import type { ClusterState } from "../gen/types_pb.ts";
import { layout } from "./layout.ts";

export function configPage(state: ClusterState): Response {
  let tableContent: string;

  if (state.config.length === 0) {
    tableContent = `<div class="empty">No configuration values</div>`;
  } else {
    const rows = state.config
      .map(
        (cv) => `<tr>
      <td>${cv.name}</td>
      <td class="mono">${cv.value}</td>
      <td style="color: var(--text-muted)">${cv.description}</td>
    </tr>`,
      )
      .join("\n");

    tableContent = `<table>
    <thead>
      <tr>
        <th>Parameter</th>
        <th>Value</th>
        <th>Description</th>
      </tr>
    </thead>
    <tbody>${rows}</tbody>
  </table>`;
  }

  const content = `
    <h1>Configuration</h1>
    ${tableContent}
  `;

  return layout("Config", content);
}
