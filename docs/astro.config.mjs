import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import tailwindcss from "@tailwindcss/vite";
import comments from "./plugins/comments.ts";

const isDev = process.argv.includes("dev");

export default defineConfig({
  site: "https://gadget-inc.github.io",
  base: "/skipper",
  vite: {
    plugins: [tailwindcss(), ...(isDev ? [comments()] : [])],
  },
  devToolbar: { enabled: false },
  integrations: [
    starlight({
      title: "Skipper",
      ...(isDev && {
        head: [
          {
            tag: "script",
            attrs: { src: "/plugins/comments-client.ts", type: "module" },
          },
        ],
      }),
      expressiveCode: {
        themes: ["github-dark-default", "github-light-default"],
        useStarlightUiThemeColors: true,
      },
      customCss: ["./src/styles/global.css"],
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/gadget-inc/skipper",
        },
      ],
      sidebar: [
        { label: "Introduction", link: "/" },
        {
          label: "Guides",
          items: [
            { label: "Deploying Functions", link: "/guides/deploying-functions/" },
            { label: "Routing and Proxying", link: "/guides/routing/" },
            { label: "Scaling", link: "/guides/scaling/" },
            { label: "Heartbeats and Lifecycle", link: "/guides/heartbeats-and-lifecycle/" },
            { label: "Observability", link: "/guides/observability/" },
          ],
        },
        {
          label: "Architecture",
          items: [
            { label: "Overview", link: "/architecture/overview/" },
            { label: "Hashing", link: "/architecture/hashing/" },
            { label: "Controller Internals", link: "/architecture/controller-internals/" },
            { label: "Router Internals", link: "/architecture/router-internals/" },
            { label: "Infrastructure", link: "/architecture/infrastructure/" },
          ],
        },
        {
          label: "Reference",
          items: [
            { label: "Configuration", link: "/reference/configuration/" },
            { label: "API", link: "/reference/api/" },
          ],
        },
        {
          label: "Contributing",
          items: [
            { label: "Getting Started", link: "/contributing/getting-started/" },
            { label: "Building and Deploying", link: "/contributing/building-and-deploying/" },
            { label: "Testing and Linting", link: "/contributing/testing/" },
            { label: "Watching Logs", link: "/contributing/watching-logs/" },
            { label: "Profiling and PGO", link: "/contributing/profiling/" },
            { label: "Contribution Workflow", link: "/contributing/workflow/" },
          ],
        },
      ],
    }),
  ],
});
