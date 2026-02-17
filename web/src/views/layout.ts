export function layout(title: string, content: string): Response {
  return new Response(
    `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>${title} — Skipper</title>
  <script src="/static/htmx.min.js"></script>
  <style>
    :root {
      --bg: #0d1117;
      --bg-secondary: #161b22;
      --border: #30363d;
      --text: #e6edf3;
      --text-muted: #8b949e;
      --accent: #58a6ff;
      --green: #3fb950;
      --yellow: #d29922;
      --red: #f85149;
      --font-mono: "SF Mono", "Cascadia Mono", "Fira Code", monospace;
    }

    * { margin: 0; padding: 0; box-sizing: border-box; }

    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
      background: var(--bg);
      color: var(--text);
      line-height: 1.5;
    }

    nav {
      background: var(--bg-secondary);
      border-bottom: 1px solid var(--border);
      padding: 12px 24px;
      display: flex;
      align-items: center;
      gap: 24px;
    }

    nav a {
      color: var(--text-muted);
      text-decoration: none;
      font-size: 14px;
      font-weight: 500;
    }

    nav a:hover, nav a.active { color: var(--text); }

    nav .logo {
      color: var(--text);
      font-weight: 700;
      font-size: 16px;
      font-family: var(--font-mono);
    }

    main { padding: 24px; max-width: 1400px; margin: 0 auto; }

    .cards {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
      gap: 16px;
      margin-bottom: 24px;
    }

    .card {
      background: var(--bg-secondary);
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 16px;
    }

    .card .label {
      font-size: 12px;
      color: var(--text-muted);
      text-transform: uppercase;
      letter-spacing: 0.05em;
      margin-bottom: 4px;
    }

    .card .value {
      font-size: 28px;
      font-weight: 700;
      font-family: var(--font-mono);
    }

    h1 {
      font-size: 20px;
      font-weight: 600;
      margin-bottom: 16px;
    }

    h2 {
      font-size: 16px;
      font-weight: 600;
      margin-bottom: 12px;
      color: var(--text-muted);
    }

    table {
      width: 100%;
      border-collapse: collapse;
      font-size: 13px;
      font-family: var(--font-mono);
    }

    thead th {
      text-align: left;
      padding: 8px 12px;
      border-bottom: 1px solid var(--border);
      color: var(--text-muted);
      font-weight: 500;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.05em;
    }

    tbody td {
      padding: 8px 12px;
      border-bottom: 1px solid var(--border);
    }

    tbody tr:hover { background: rgba(88, 166, 255, 0.04); }

    a { color: var(--accent); text-decoration: none; }
    a:hover { text-decoration: underline; }

    .badge {
      display: inline-block;
      padding: 2px 8px;
      border-radius: 12px;
      font-size: 11px;
      font-weight: 500;
    }

    .badge-green { background: rgba(63, 185, 80, 0.15); color: var(--green); }
    .badge-yellow { background: rgba(210, 153, 34, 0.15); color: var(--yellow); }
    .badge-red { background: rgba(248, 81, 73, 0.15); color: var(--red); }
    .badge-muted { background: rgba(139, 148, 158, 0.15); color: var(--text-muted); }

    .section { margin-bottom: 32px; }

    .empty {
      text-align: center;
      padding: 32px;
      color: var(--text-muted);
    }

    .mono { font-family: var(--font-mono); }

    .health-indicators {
      display: flex;
      flex-wrap: wrap;
      gap: 12px;
      margin-bottom: 24px;
      padding: 12px 16px;
      background: var(--bg-secondary);
      border: 1px solid var(--border);
      border-radius: 8px;
    }

    .health-indicator {
      display: flex;
      align-items: center;
      gap: 8px;
      font-size: 13px;
    }

    .health-dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      flex-shrink: 0;
    }

    .health-dot-green { background: var(--green); }
    .health-dot-yellow { background: var(--yellow); }
    .health-dot-red { background: var(--red); }

    .timeline {
      display: flex;
      align-items: center;
      gap: 0;
      margin-bottom: 24px;
      padding: 16px;
      background: var(--bg-secondary);
      border: 1px solid var(--border);
      border-radius: 8px;
    }

    .timeline-step {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 4px;
      min-width: 80px;
    }

    .timeline-step .step-dot {
      width: 12px;
      height: 12px;
      border-radius: 50%;
      background: var(--green);
      border: 2px solid var(--bg);
    }

    .timeline-step .step-dot.inactive {
      background: var(--border);
    }

    .timeline-step .step-label {
      font-size: 11px;
      color: var(--text-muted);
      text-transform: uppercase;
    }

    .timeline-connector {
      flex: 1;
      height: 2px;
      background: var(--border);
      position: relative;
      min-width: 40px;
    }

    .timeline-connector.active { background: var(--green); }

    .timeline-duration {
      font-size: 11px;
      color: var(--text-muted);
      text-align: center;
      padding: 0 8px;
    }

    .timeline-duration.slow { color: var(--yellow); }

    .banner {
      padding: 8px 16px;
      border-radius: 6px;
      font-size: 13px;
      margin-bottom: 16px;
    }

    .banner-yellow {
      background: rgba(210, 153, 34, 0.1);
      border: 1px solid rgba(210, 153, 34, 0.3);
      color: var(--yellow);
    }

    .banner-red {
      background: rgba(248, 81, 73, 0.1);
      border: 1px solid rgba(248, 81, 73, 0.3);
      color: var(--red);
    }

    .dist-chart { margin-bottom: 16px; }

    .dist-row {
      display: flex;
      align-items: center;
      gap: 12px;
      margin-bottom: 4px;
      font-size: 13px;
      font-family: var(--font-mono);
    }

    .dist-label { min-width: 120px; color: var(--text-muted); }

    .dist-bar-bg {
      flex: 1;
      height: 20px;
      background: var(--bg);
      border-radius: 4px;
      overflow: hidden;
    }

    .dist-bar {
      height: 100%;
      background: var(--accent);
      border-radius: 4px;
      transition: width 0.3s;
    }

    .dist-count {
      min-width: 30px;
      text-align: right;
    }

    .filter-bar {
      display: flex;
      gap: 12px;
      margin-bottom: 16px;
      align-items: center;
    }

    .filter-input, .filter-select {
      background: var(--bg);
      border: 1px solid var(--border);
      border-radius: 6px;
      color: var(--text);
      padding: 6px 12px;
      font-size: 13px;
      font-family: var(--font-mono);
    }

    .filter-input:focus, .filter-select:focus {
      outline: none;
      border-color: var(--accent);
    }

    .filter-select { cursor: pointer; }

    tr.row-highlight { background: rgba(210, 153, 34, 0.06); }
  </style>
</head>
<body>
  <nav>
    <a href="/" class="logo">skipper</a>
    <a href="/">Dashboard</a>
    <a href="/functions">Functions</a>
    <a href="/controllers">Controllers</a>
    <a href="/routers">Routers</a>
    <a href="/events">Events</a>
    <a href="/config">Config</a>
  </nav>
  <main>
    ${content}
  </main>
</body>
</html>`,
    { headers: { "content-type": "text/html; charset=utf-8" } },
  );
}
