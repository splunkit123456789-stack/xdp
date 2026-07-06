package mvp

import "net/http"

func (h *Handler) web(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>XDP Console</title>
  <style>
    :root {
      --black: #0b0f0d;
      --black-2: #171d1a;
      --black-3: #252b28;
      --green: #65a637;
      --green-dark: #4d8428;
      --orange: #f28c28;
      --blue: #2c6f91;
      --red: #c84a3a;
      --text: #222827;
      --muted: #68716f;
      --bg: #eef0ed;
      --panel: #ffffff;
      --line: #d5dbd8;
      --line-dark: #303936;
      --mono: "Cascadia Code", "SFMono-Regular", "Liberation Mono", monospace;
      --sans: "Aptos", "Avenir Next", "Helvetica Neue", sans-serif;
      --shadow: 0 12px 26px rgba(11, 15, 13, 0.12);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      color: var(--text);
      font-family: var(--sans);
      background:
        linear-gradient(180deg, rgba(11, 15, 13, 0.06), transparent 220px),
        radial-gradient(circle at 85% 0%, rgba(101, 166, 55, 0.15), transparent 25rem),
        var(--bg);
    }
    button, input, select, textarea { font: inherit; }
    button {
      border: 1px solid transparent;
      border-radius: 3px;
      padding: 8px 14px;
      color: #fff;
      background: var(--green);
      cursor: pointer;
      font-weight: 700;
      line-height: 1.2;
    }
    button:hover { background: var(--green-dark); }
    button.secondary {
      color: var(--text);
      background: #f7f8f7;
      border-color: #bfc8c4;
    }
    button.secondary:hover { background: #e8ecea; }
    button.danger { background: var(--red); }
    button.blue { background: var(--blue); }
    button.link {
      color: #dfe9e4;
      background: transparent;
      border-color: transparent;
      padding: 5px 8px;
    }
    input, select, textarea {
      width: 100%;
      border: 1px solid #bfc8c4;
      border-radius: 3px;
      padding: 8px 9px;
      color: var(--text);
      background: #fff;
      outline: none;
    }
    input:focus, select:focus, textarea:focus {
      border-color: var(--green);
      box-shadow: 0 0 0 2px rgba(101, 166, 55, 0.18);
    }
    textarea {
      min-height: 112px;
      resize: vertical;
      font-family: var(--mono);
      font-size: 12px;
      line-height: 1.55;
    }
    label {
      display: block;
      margin: 0 0 5px;
      color: #4d5653;
      font-size: 12px;
      font-weight: 800;
    }
    pre {
      margin: 0;
      white-space: pre-wrap;
      word-break: break-word;
      font-family: var(--mono);
      font-size: 12px;
      line-height: 1.55;
    }
    .muted { color: var(--muted); }
    .mono { font-family: var(--mono); font-size: 12px; overflow-wrap: anywhere; }
    .login-shell {
      min-height: 100vh;
      display: grid;
      grid-template-rows: 56px minmax(0, 1fr);
      background:
        linear-gradient(135deg, rgba(11, 15, 13, 0.94), rgba(23, 29, 26, 0.97)),
        radial-gradient(circle at 80% 18%, rgba(101, 166, 55, 0.22), transparent 24rem);
    }
    .login-top {
      display: flex;
      align-items: center;
      padding: 0 28px;
      border-bottom: 1px solid var(--line-dark);
      color: #eef4ef;
      background: rgba(0, 0, 0, 0.22);
    }
    .logo {
      display: inline-flex;
      align-items: center;
      gap: 2px;
      font-size: 24px;
      font-weight: 900;
      letter-spacing: -0.06em;
    }
    .logo .mark { color: var(--green); padding-left: 2px; }
    .login-main {
      width: min(1120px, calc(100% - 32px));
      margin: 0 auto;
      display: grid;
      grid-template-columns: minmax(0, 1fr) 392px;
      gap: 36px;
      align-items: center;
      padding: 42px 0;
    }
    .login-copy {
      color: #eef4ef;
      padding: 38px 0;
    }
    .login-copy h1 {
      margin: 0;
      max-width: 760px;
      font-size: clamp(44px, 6vw, 86px);
      line-height: 0.94;
      letter-spacing: -0.07em;
    }
    .login-copy p {
      max-width: 720px;
      margin: 22px 0 0;
      color: #bbc8c2;
      font-size: 17px;
      line-height: 1.75;
    }
    .login-card {
      border: 1px solid #313a36;
      border-radius: 4px;
      background: #f9faf9;
      box-shadow: 0 28px 80px rgba(0, 0, 0, 0.35);
      padding: 26px;
    }
    .login-card h2 {
      margin: 0 0 18px;
      font-size: 22px;
    }
    .login-card .login-actions {
      display: flex;
      gap: 10px;
      margin-top: 16px;
    }
    .app-shell[hidden], .login-shell[hidden], .view[hidden] { display: none; }
    .app-shell {
      min-height: 100vh;
      display: grid;
      grid-template-rows: 48px 46px minmax(0, 1fr);
    }
    .global-bar {
      display: flex;
      align-items: center;
      gap: 18px;
      padding: 0 18px;
      color: #e7eee9;
      background: var(--black);
      border-bottom: 1px solid #222a27;
    }
    .global-bar .logo { font-size: 22px; }
    .app-name {
      display: flex;
      align-items: center;
      gap: 9px;
      color: #dce5df;
      font-weight: 700;
    }
    .global-spacer { flex: 1; }
    .global-meta {
      display: flex;
      align-items: center;
      gap: 12px;
      color: #aebbb5;
      font-size: 12px;
    }
    .module-bar {
      display: flex;
      align-items: center;
      gap: 2px;
      padding: 0 18px;
      background: var(--black-2);
      border-bottom: 1px solid var(--line-dark);
    }
    .module-tab {
      height: 46px;
      border: 0;
      border-radius: 0;
      padding: 0 18px;
      color: #d5ded9;
      background: transparent;
      box-shadow: none;
      font-weight: 800;
    }
    .module-tab:hover { background: #232b27; }
    .module-tab.active {
      color: #fff;
      background: #303834;
      box-shadow: inset 0 -3px 0 var(--green);
    }
    .workspace {
      min-width: 0;
      width: min(1680px, 100%);
      margin: 0 auto;
      padding: 18px;
    }
    .page-head {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(420px, auto);
      gap: 18px;
      align-items: end;
      margin-bottom: 14px;
    }
    .page-title h1 {
      margin: 0;
      font-size: 28px;
      letter-spacing: -0.035em;
    }
    .page-title p {
      margin: 6px 0 0;
      color: var(--muted);
      line-height: 1.55;
    }
    .auth-controls {
      display: grid;
      grid-template-columns: 140px 150px auto;
      gap: 10px;
      align-items: end;
    }
    .status-strip {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 10px;
      margin-bottom: 14px;
    }
    .metric {
      border: 1px solid var(--line);
      background: #fff;
      padding: 12px 14px;
      border-radius: 4px;
      box-shadow: 0 1px 0 rgba(0,0,0,0.02);
    }
    .metric span {
      display: block;
      color: var(--muted);
      font-size: 12px;
      font-weight: 800;
    }
    .metric strong {
      display: block;
      margin-top: 6px;
      font-size: 20px;
    }
    .dot {
      display: inline-block;
      width: 9px;
      height: 9px;
      border-radius: 50%;
      margin-right: 6px;
      background: var(--orange);
    }
    .dot.ok { background: var(--green); }
    .dot.bad { background: var(--red); }
    .layout {
      display: grid;
      grid-template-columns: minmax(360px, 0.78fr) minmax(0, 1.22fr);
      gap: 14px;
    }
    .search-layout {
      display: grid;
      grid-template-columns: 270px minmax(0, 1fr);
      gap: 14px;
    }
    .stack { display: grid; gap: 14px; }
    .panel {
      border: 1px solid var(--line);
      border-radius: 4px;
      background: var(--panel);
      box-shadow: var(--shadow);
    }
    .panel-head {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      padding: 12px 14px;
      border-bottom: 1px solid var(--line);
      background: #f8f9f8;
    }
    .panel-head h2 {
      margin: 0;
      font-size: 16px;
    }
    .panel-body { padding: 14px; }
    .settings-layout {
      display: grid;
      grid-template-columns: 230px minmax(0, 1fr);
      gap: 14px;
    }
    .settings-nav {
      border: 1px solid var(--line);
      border-radius: 4px;
      background: #fff;
      overflow: hidden;
      height: max-content;
    }
    .settings-nav h3 {
      margin: 0;
      padding: 12px 14px;
      font-size: 13px;
      background: #f7f8f7;
      border-bottom: 1px solid var(--line);
    }
    .settings-nav div {
      padding: 11px 14px;
      border-bottom: 1px solid #edf0ee;
      color: #3f4845;
      font-size: 13px;
    }
    .settings-nav div.active {
      border-left: 4px solid var(--green);
      padding-left: 10px;
      color: #111;
      font-weight: 800;
      background: #fbfdfb;
    }
    .grid-2 {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 12px;
    }
    .grid-3 {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 12px;
    }
    .field { margin-bottom: 12px; }
    .hint {
      margin-top: 8px;
      color: var(--muted);
      font-size: 12px;
      line-height: 1.55;
    }
    .message {
      min-height: 20px;
      margin-top: 9px;
      color: var(--muted);
      font-size: 13px;
    }
    .message.ok { color: var(--green-dark); }
    .message.bad { color: var(--red); }
    .button-row {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      align-items: center;
    }
    .data-input-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 12px;
      margin-bottom: 14px;
    }
    .input-type-card {
      border: 1px solid #cfd7d3;
      border-radius: 4px;
      padding: 14px;
      background: linear-gradient(180deg, #fff, #f7faf6);
    }
    .input-type-card span {
      color: var(--green-dark);
      font-size: 12px;
      font-weight: 900;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }
    .input-type-card strong {
      display: block;
      margin: 7px 0;
      font-size: 18px;
    }
    .table-wrap {
      overflow: auto;
      border: 1px solid var(--line);
      border-radius: 4px;
      background: #fff;
    }
    table {
      width: 100%;
      min-width: 900px;
      border-collapse: collapse;
      font-size: 13px;
    }
    th, td {
      padding: 10px 11px;
      border-bottom: 1px solid #e5e9e7;
      text-align: left;
      vertical-align: top;
    }
    th {
      color: #52605b;
      font-size: 12px;
      background: #f3f5f4;
      position: sticky;
      top: 0;
      z-index: 1;
    }
    tr:hover td { background: #fbfcfb; }
    .empty {
      padding: 34px;
      color: var(--muted);
      text-align: center;
    }
    .raw-box {
      min-height: 250px;
      max-height: 620px;
      overflow: auto;
      border-radius: 4px;
      background: #111816;
      color: #dff2e3;
      padding: 14px;
    }
    .search-command {
      border: 1px solid #bcc7c1;
      border-radius: 4px;
      background: #fff;
      box-shadow: var(--shadow);
      margin-bottom: 14px;
    }
    .search-command-head {
      display: grid;
      grid-template-columns: minmax(0, 1fr) 230px 98px;
      gap: 8px;
      padding: 12px;
      background: #fff;
    }
    .spl-input {
      height: 42px;
      font-family: var(--mono);
      font-size: 16px;
      border-color: #9eaaa5;
    }
    .search-options {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 10px;
      padding: 0 12px 12px;
      border-top: 1px solid #eef1ef;
    }
    .field-sidebar {
      border: 1px solid var(--line);
      border-radius: 4px;
      background: #fff;
      box-shadow: var(--shadow);
      height: max-content;
      overflow: hidden;
    }
    .field-sidebar h2 {
      margin: 0;
      padding: 12px 14px;
      font-size: 16px;
      background: #f8f9f8;
      border-bottom: 1px solid var(--line);
    }
    .field-list { padding: 10px 0; }
    .field-item {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 8px;
      padding: 8px 14px;
      border-bottom: 1px solid #f0f2f1;
      cursor: pointer;
    }
    .field-item:hover { background: #f7faf6; }
    .field-item strong {
      font-family: var(--mono);
      font-size: 12px;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .field-item span {
      color: var(--muted);
      font-size: 11px;
    }
    .tabs {
      display: flex;
      gap: 0;
      border-bottom: 1px solid var(--line);
      background: #f7f8f7;
    }
    .tab {
      color: #3f4945;
      background: transparent;
      border: 0;
      border-right: 1px solid var(--line);
      border-radius: 0;
      box-shadow: none;
      padding: 11px 15px;
    }
    .tab:hover { background: #eef2ef; }
    .tab.active {
      color: #111;
      background: #fff;
      box-shadow: inset 0 3px 0 var(--green);
    }
    .tab-content { padding: 14px; background: #fff; }
    .bar {
      display: grid;
      grid-template-columns: 170px minmax(40px, 1fr) 48px;
      gap: 10px;
      align-items: center;
      margin: 8px 0;
      font-size: 12px;
    }
    .bar-track {
      height: 12px;
      border-radius: 999px;
      background: #edf1ef;
      overflow: hidden;
    }
    .bar-fill {
      height: 100%;
      border-radius: 999px;
      background: linear-gradient(90deg, var(--green), var(--orange));
    }
    @media (max-width: 1180px) {
      .page-head, .layout, .settings-layout, .search-layout, .login-main { grid-template-columns: 1fr; }
      .auth-controls, .search-command-head, .search-options { grid-template-columns: 1fr; }
      .status-strip, .grid-2, .grid-3, .data-input-grid { grid-template-columns: 1fr; }
      .module-bar { overflow-x: auto; }
    }
    @media (max-width: 720px) {
      .workspace { padding: 10px; }
      .global-meta { display: none; }
      button { width: 100%; }
      .button-row { display: grid; }
      .login-copy h1 { font-size: 42px; }
    }
  </style>
</head>
<body>
  <section id="loginScreen" class="login-shell">
    <div class="login-top"><div class="logo">XDP<span class="mark">&gt;</span></div></div>
    <div class="login-main">
      <div class="login-copy">
        <h1>Search, monitor, and manage your machine data.</h1>
        <p>参考 Splunk Web 的工作流：登录后进入数据输入、字段解析、索引管理与 SPL 搜索。XDP 保留自己的品牌与能力模型，不复制第三方资产。</p>
      </div>
      <div class="login-card">
        <h2>Sign in to XDP</h2>
        <div class="field"><label for="loginUser">Username</label><input id="loginUser" value="admin" autocomplete="username"></div>
        <div class="field"><label for="loginPass">Password</label><input id="loginPass" type="password" value="xdp" autocomplete="current-password"></div>
        <div class="login-actions">
          <button onclick="login()">Sign In</button>
          <button id="devEnter" class="secondary" onclick="enterApp()" hidden>Skip for dev</button>
        </div>
        <div id="loginMessage" class="message"></div>
        <div class="hint">认证启用时使用后端配置账号；未启用认证时可直接进入。</div>
      </div>
    </div>
  </section>

  <section id="appShell" class="app-shell" hidden>
    <header class="global-bar">
      <div class="logo">XDP<span class="mark">&gt;</span></div>
      <div class="app-name">Search & Reporting</div>
      <div class="global-spacer"></div>
      <div class="global-meta"><span id="healthText"><span id="healthDot" class="dot"></span>checking</span><span id="authMode">auth</span></div>
      <button class="link" onclick="logout()">Logout</button>
    </header>
    <nav class="module-bar">
      <button id="navCollect" class="module-tab active" onclick="showPage('collect')">Data Inputs</button>
      <button id="navParse" class="module-tab" onclick="showPage('parse')">Fields</button>
      <button id="navIndexes" class="module-tab" onclick="showPage('indexes')">Indexes</button>
      <button id="navSearch" class="module-tab" onclick="showPage('search')">Search</button>
    </nav>

    <main class="workspace">
      <div class="page-head">
        <div class="page-title">
          <h1 id="pageTitle">Data Inputs</h1>
          <p id="pageSubtitle">Configure supported collection inputs and bind them to indexes and runtime pipelines.</p>
        </div>
        <div class="auth-controls">
          <div class="field"><label for="apiToken">API Token</label><input id="apiToken" type="password" placeholder="Bearer token"></div>
          <div class="button-row">
            <button class="secondary" onclick="saveManualToken()">Save Token</button>
            <button class="secondary" onclick="refreshAll()">Refresh</button>
          </div>
        </div>
      </div>

      <div class="status-strip">
        <div class="metric"><span>Data inputs</span><strong id="sourceCount">-</strong></div>
        <div class="metric"><span>Indexes</span><strong id="indexCount">-</strong></div>
        <div class="metric"><span>Last result</span><strong id="resultCount">-</strong></div>
        <div class="metric"><span>Mode</span><strong id="modeText">Console</strong></div>
      </div>

      <section id="viewCollect" class="view settings-layout">
        <aside class="settings-nav">
          <h3>Settings</h3>
          <div class="active">Data inputs</div>
          <div>Syslog UDP</div>
          <div>Runtime pipeline</div>
        </aside>
        <div class="stack">
          <div class="panel">
            <div class="panel-head"><h2>Add Data</h2><button onclick="resetCollectionForm()">New Input</button></div>
            <div class="panel-body">
              <div class="data-input-grid">
                <div class="input-type-card"><span>Syslog UDP</span><strong>Network and security data</strong><div class="hint">默认 :5514/udp，适合防火墙、网络设备和安全日志。</div></div>
              </div>
              <div class="grid-3">
                <div class="field"><label for="collID">Input ID</label><input id="collID" value="syslog-default"></div>
                <div class="field"><label for="collType">Source type</label><select id="collType"><option value="syslog">Syslog UDP</option></select></div>
                <div class="field"><label for="collStatus">Status</label><select id="collStatus"><option value="active">active</option><option value="disabled">disabled</option></select></div>
              </div>
              <div class="grid-3">
                <div class="field"><label for="collName">Name</label><input id="collName" value="Default Syslog"></div>
                <div class="field"><label for="collAddr">Listen address</label><input id="collAddr" value=":5514"></div>
                <div class="field"><label for="collEndpoint">Protocol</label><input id="collEndpoint" value="UDP"></div>
              </div>
              <div class="grid-3">
                <div class="field"><label for="collIndex">Default index</label><input id="collIndex" value="app"></div>
                <div class="field"><label for="collTimeField">Timestamp field</label><input id="collTimeField" value=""></div>
                <div class="field"><label for="collParser">Parser</label><select id="collParser"><option value="regex">regex</option></select></div>
              </div>
              <div class="grid-2">
                <div class="field"><label for="collPipelineID">Pipeline ID</label><input id="collPipelineID" value="syslog-collection-pipeline"></div>
              </div>
              <div class="field"><label for="collRegex">Regex pattern</label><textarea id="collRegex" placeholder="src=(?<src_ip>\S+) dst=(?<dst_ip>\S+) action=(?<action>\S+) bytes=(?<bytes>\d+)"></textarea></div>
              <div class="button-row">
                <button onclick="saveCollectionConfig()">Save</button>
                <button class="secondary" onclick="loadDataSources()">Reload list</button>
              </div>
              <div id="collectMessage" class="message"></div>
            </div>
          </div>
          <div class="panel">
            <div class="panel-head"><h2>Configured Inputs</h2><span class="muted">Saved in metadata store</span></div>
            <div class="panel-body"><div id="sourcesView"></div></div>
          </div>
        </div>
      </section>

      <section id="viewParse" class="view settings-layout" hidden>
        <aside class="settings-nav">
          <h3>Settings</h3>
          <div>Data inputs</div>
          <div class="active">Fields</div>
          <div>Field aliases</div>
          <div>Calculated fields</div>
        </aside>
        <div class="layout">
          <div class="panel">
            <div class="panel-head"><h2>Field extraction and normalization</h2></div>
            <div class="panel-body">
              <div class="field"><label for="parseSource">Data input</label><select id="parseSource" onchange="loadParseForm()"></select></div>
              <div class="grid-2">
                <div class="field"><label for="parseParser">Parser</label><select id="parseParser"><option value="regex">regex</option></select></div>
                <div class="field"><label for="parseTimeField">Timestamp config</label><input id="parseTimeField" value="" placeholder="Use props.conf TIME_* settings"></div>
              </div>
              <div class="field"><label for="parseRegex">Regex pattern</label><textarea id="parseRegex"></textarea></div>
              <div class="grid-2">
                <div class="field"><label for="parseFieldMapping">Field mapping JSON</label><textarea id="parseFieldMapping">{
  "src": "src_ip",
  "dst": "dst_ip"
}</textarea></div>
                <div class="field"><label for="parseTypeMapping">Type normalization JSON</label><textarea id="parseTypeMapping">{
  "bytes": "int"
}</textarea></div>
              </div>
              <div class="button-row">
                <button onclick="saveParsingConfig()">Save field settings</button>
                <button class="secondary" onclick="previewRuntimePipelines()">Preview pipeline</button>
              </div>
              <div class="hint">保存后会生成 field-mapping 与 type-convert stage，并被 Worker 热加载。</div>
              <div id="parseMessage" class="message"></div>
            </div>
          </div>
          <div class="panel">
            <div class="panel-head"><h2>Runtime Pipeline Preview</h2></div>
            <div class="panel-body"><div class="raw-box"><pre id="pipelinePreview">Select a data input to preview generated pipeline.</pre></div></div>
          </div>
        </div>
      </section>

      <section id="viewIndexes" class="view settings-layout" hidden>
        <aside class="settings-nav">
          <h3>Settings</h3>
          <div>Server settings</div>
          <div class="active">Indexes</div>
          <div>Retention</div>
          <div>Storage</div>
        </aside>
        <div class="stack">
          <div class="panel">
            <div class="panel-head"><h2>Indexes</h2><button onclick="saveIndexConfig()">New Index</button></div>
            <div class="panel-body">
              <div class="grid-3">
                <div class="field"><label for="indexName">Index name</label><input id="indexName" placeholder="app"></div>
                <div class="field"><label for="indexDisplayName">Display name</label><input id="indexDisplayName" placeholder="Application Logs"></div>
                <div class="field"><label for="indexTTL">TTL days</label><input id="indexTTL" type="number" value="30"></div>
              </div>
              <div class="grid-2">
                <div class="field"><label for="indexStatus">Status</label><select id="indexStatus"><option value="active">active</option><option value="disabled">disabled</option></select></div>
                <div class="field"><label for="indexDropStorage">Delete option</label><select id="indexDropStorage"><option value="false">Delete config only</option><option value="true">Drop ClickHouse table too</option></select></div>
              </div>
              <div class="grid-2">
                <div class="field"><label>&nbsp;</label><div class="button-row"><button class="secondary" onclick="loadIndexes()">Refresh indexes</button></div></div>
              </div>
              <div class="hint">ClickHouse 模式下创建 events_&lt;index&gt; 物理表。Index 仅支持小写字母、数字、下划线。</div>
              <div id="indexMessage" class="message"></div>
            </div>
          </div>
          <div class="panel">
            <div class="panel-head"><h2>Index List</h2><span class="muted">Rows, latest event, retention and storage</span></div>
            <div class="panel-body"><div id="indexesView"></div></div>
          </div>
        </div>
      </section>

      <section id="viewSearch" class="view" hidden>
        <div class="search-command">
          <div class="search-command-head">
            <input id="spl" class="spl-input" value="index=app | stats count as total by service" aria-label="SPL query">
            <select id="rangePreset" onchange="applyRangePreset()">
              <option value="24h">Last 24 hours</option>
              <option value="15m">Last 15 minutes</option>
              <option value="7d">Last 7 days</option>
              <option value="all">All time</option>
              <option value="custom">Custom time</option>
            </select>
            <button onclick="runSPL()">Search</button>
          </div>
          <div class="search-options">
            <div class="field"><label for="startTime">Earliest</label><input id="startTime" type="datetime-local"></div>
            <div class="field"><label for="endTime">Latest</label><input id="endTime" type="datetime-local"></div>
            <div class="field"><label for="limit">Limit</label><input id="limit" value="100"></div>
            <div class="field"><label for="page">Page</label><input id="page" value="1"></div>
          </div>
        </div>
        <div class="search-layout">
          <aside class="field-sidebar">
            <h2>Fields</h2>
            <div class="field-list" id="searchFieldsRail"><div class="empty">Run a search or click Fields.</div></div>
            <div style="padding:0 14px 14px;"><button class="secondary" onclick="loadFields()">Refresh fields</button></div>
          </aside>
          <div class="panel">
            <div class="panel-head">
              <h2>Search Results</h2>
              <div class="button-row">
                <button class="secondary" onclick="prevPage()">Previous</button>
                <button class="secondary" onclick="nextPage()">Next</button>
                <button class="secondary" onclick="loadTimeline()">Timeline</button>
              </div>
            </div>
            <div class="tabs">
              <button id="tabEvents" class="tab active" onclick="showTab('events')">Events</button>
              <button id="tabStats" class="tab" onclick="showTab('stats')">Statistics</button>
              <button id="tabTimeline" class="tab" onclick="showTab('timeline')">Visualization</button>
              <button id="tabFields" class="tab" onclick="showTab('fields')">Fields</button>
              <button id="tabRaw" class="tab" onclick="showTab('raw')">Raw JSON</button>
            </div>
            <div class="tab-content">
              <div id="eventsView"></div>
              <div id="statsView" hidden></div>
              <div id="timelineView" hidden></div>
              <div id="fieldsView" hidden></div>
              <div id="rawView" hidden><div class="raw-box"><pre id="rawResponse">No response yet.</pre></div></div>
            </div>
            <div id="searchMessage" class="message" style="padding:0 14px 14px;"></div>
          </div>
        </div>
      </section>
    </main>
  </section>

<script>
const state = {
  token: localStorage.getItem("xdp_token") || "",
  auth: {enabled: false},
  sources: [],
  indexes: [],
  currentRows: [],
  currentResult: null
};

function byID(id) { return document.getElementById(id); }
function cap(value) { return value.charAt(0).toUpperCase() + value.slice(1); }
function escapeHTML(value) {
  return String(value == null ? "" : value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}
function setMessage(id, text, kind) {
  const item = byID(id);
  if (!item) return;
  item.textContent = text || "";
  item.className = "message " + (kind || "");
}
function rememberRaw(value) {
  const raw = byID("rawResponse");
  if (raw) raw.textContent = typeof value === "string" ? value : JSON.stringify(value, null, 2);
}
function authHeaders(extra) {
  const headers = Object.assign({}, extra || {});
  const token = state.token || byID("apiToken")?.value || "";
  if (token) headers.Authorization = "Bearer " + token;
  return headers;
}
async function fetchJSON(url, options) {
  const opts = Object.assign({}, options || {});
  opts.headers = authHeaders(opts.headers);
  const res = await fetch(url, opts);
  const text = await res.text();
  let body;
  try { body = text ? JSON.parse(text) : {}; } catch (e) { body = text; }
  rememberRaw(body);
  if (!res.ok) {
    if (res.status === 401) showLogin();
    const message = body && body.error && body.error.message
      ? body.error.message
      : body && typeof body.error === "string"
        ? body.error
        : text || res.statusText;
    throw new Error(message);
  }
  return body;
}
async function loadPublicAuth() {
  const res = await fetch("/api/v1/auth");
  const body = await res.json();
  state.auth = body;
  return body;
}
async function init() {
  byID("apiToken").value = state.token;
  applyRangePreset();
  try {
    const auth = await loadPublicAuth();
    byID("devEnter").hidden = !!auth.enabled;
    byID("authMode").textContent = auth.enabled ? "Auth enabled" : "Auth disabled";
    if (state.token && auth.enabled) {
      enterApp();
    } else {
      showLogin();
      if (!auth.enabled) setMessage("loginMessage", "认证未启用，可直接进入。", "ok");
    }
  } catch (err) {
    showLogin();
    setMessage("loginMessage", "认证状态检测失败：" + err.message, "bad");
  }
}
function showLogin() {
  byID("loginScreen").hidden = false;
  byID("appShell").hidden = true;
}
async function login() {
  try {
    const body = {username: byID("loginUser").value, password: byID("loginPass").value};
    const result = await fetchJSON("/api/v1/login", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(body)});
    state.token = result.token || "";
    localStorage.setItem("xdp_token", state.token);
    byID("apiToken").value = state.token;
    enterApp();
  } catch (err) {
    setMessage("loginMessage", err.message, "bad");
  }
}
function enterApp() {
  byID("loginScreen").hidden = true;
  byID("appShell").hidden = false;
  showPage("search");
  refreshAll();
}
function logout() {
  state.token = "";
  localStorage.removeItem("xdp_token");
  byID("apiToken").value = "";
  showLogin();
}
function saveManualToken() {
  state.token = byID("apiToken").value.trim();
  localStorage.setItem("xdp_token", state.token);
}
function showPage(name) {
  const titles = {
    collect: ["Data Inputs", "Configure supported collection methods and route data into indexes."],
    parse: ["Fields", "Extract, map and normalize fields before events are indexed."],
    indexes: ["Indexes", "Create, inspect and remove index metadata and physical tables."],
    search: ["Search", "Run SPL queries with a time range picker, fields sidebar and result tabs."]
  };
  ["collect", "parse", "indexes", "search"].forEach(function(item) {
    byID("view" + cap(item)).hidden = item !== name;
    byID("nav" + cap(item)).classList.toggle("active", item === name);
  });
  byID("pageTitle").textContent = titles[name][0];
  byID("pageSubtitle").textContent = titles[name][1];
  if (name === "collect") loadDataSources();
  if (name === "parse") { loadDataSources().then(loadParseForm); previewRuntimePipelines(); }
  if (name === "indexes") loadIndexes();
}
async function refreshAll() {
  await checkHealth();
  await loadDataSources();
  await loadIndexes();
}
async function checkHealth() {
  try {
    const result = await fetchJSON("/healthz");
    byID("healthDot").className = "dot ok";
    byID("healthText").innerHTML = '<span id="healthDot" class="dot ok"></span>' + escapeHTML(result.status || "ok");
  } catch (err) {
    byID("healthText").innerHTML = '<span id="healthDot" class="dot bad"></span>down';
  }
}
function renderTable(targetID, rows, columns, emptyText) {
  const target = byID(targetID);
  if (!rows || rows.length === 0) {
    target.innerHTML = '<div class="empty">' + escapeHTML(emptyText) + '</div>';
    return;
  }
  const head = columns.map(function(col) { return '<th>' + escapeHTML(col.label) + '</th>'; }).join("");
  const body = rows.map(function(row) {
    return '<tr>' + columns.map(function(col) {
      const value = col.value(row);
      const cls = col.mono ? ' class="mono"' : "";
      return '<td' + cls + '>' + (col.html ? value : escapeHTML(value == null ? "" : value)) + '</td>';
    }).join("") + '</tr>';
  }).join("");
  target.innerHTML = '<div class="table-wrap"><table><thead><tr>' + head + '</tr></thead><tbody>' + body + '</tbody></table></div>';
}
async function loadDataSources() {
  try {
    const result = await fetchJSON("/api/v1/datasources");
    state.sources = result.datasources || [];
    byID("sourceCount").textContent = state.sources.length;
    renderDataSources();
    populateParseSources();
  } catch (err) {
    const target = byID("sourcesView");
    if (target) target.innerHTML = '<div class="empty">' + escapeHTML(err.message) + '</div>';
  }
}
function renderDataSources() {
  if (!byID("sourcesView")) return;
  renderTable("sourcesView", state.sources, [
    {label: "Name", value: row => row.name},
    {label: "Input ID", value: row => row.id, mono: true},
    {label: "Type", value: row => row.type},
    {label: "Status", value: row => row.status},
    {label: "Index", value: row => row.default_index},
    {label: "Timestamp", value: row => row.time_field || "-"},
    {label: "Parser", value: row => row.parser},
    {label: "Actions", html: true, value: row => '<button class="secondary" onclick="editSource(\'' + encodeURIComponent(row.id) + '\')">Edit</button>'}
  ], "No data inputs configured");
}
function resetCollectionForm() {
  byID("collID").value = "";
  byID("collType").value = "syslog";
  byID("collName").value = "";
  byID("collStatus").value = "active";
  byID("collAddr").value = ":5514";
  byID("collEndpoint").value = "UDP";
  byID("collIndex").value = "app";
  byID("collTimeField").value = "";
  byID("collParser").value = "regex";
  byID("collPipelineID").value = "";
  byID("collRegex").value = "";
}
function editSource(encodedID) {
  const id = decodeURIComponent(encodedID);
  const source = state.sources.find(item => item.id === id);
  if (!source) return;
  byID("collID").value = source.id || "";
  byID("collType").value = source.type || "syslog";
  byID("collName").value = source.name || "";
  byID("collStatus").value = source.status || "active";
  byID("collAddr").value = source.addr || "";
  byID("collEndpoint").value = source.protocol || "UDP";
  byID("collIndex").value = source.default_index || "app";
  byID("collTimeField").value = source.time_field || "";
  byID("collParser").value = source.parser || "regex";
  byID("collPipelineID").value = source.pipeline_id || "";
  byID("collRegex").value = source.regex_pattern || "";
  showPage("collect");
}
function sourceFromCollectionForm() {
  const type = "syslog";
  const endpoint = byID("collEndpoint").value.trim();
  return {
    id: byID("collID").value.trim(),
    type: type,
    name: byID("collName").value.trim(),
    status: byID("collStatus").value,
    addr: byID("collAddr").value.trim(),
    path: "",
    protocol: endpoint,
    default_index: byID("collIndex").value.trim(),
    time_field: byID("collTimeField").value.trim(),
    parser: byID("collParser").value,
    regex_pattern: byID("collRegex").value,
    pipeline_id: byID("collPipelineID").value.trim()
  };
}
async function saveCollectionConfig() {
  try {
    const result = await fetchJSON("/api/v1/datasources", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(sourceFromCollectionForm())});
    setMessage("collectMessage", "Saved input: " + result.id, "ok");
    await loadDataSources();
    await loadIndexes();
  } catch (err) {
    setMessage("collectMessage", err.message, "bad");
  }
}
function populateParseSources() {
  const select = byID("parseSource");
  if (!select) return;
  const current = select.value;
  select.innerHTML = state.sources.map(function(item) {
    return '<option value="' + escapeHTML(item.id) + '">' + escapeHTML(item.id + " / " + item.type) + '</option>';
  }).join("");
  if (current && state.sources.some(item => item.id === current)) select.value = current;
}
function loadParseForm() {
  const id = byID("parseSource").value;
  const source = state.sources.find(item => item.id === id) || state.sources[0];
  if (!source) return;
  byID("parseSource").value = source.id;
  byID("parseParser").value = source.parser || "regex";
  byID("parseTimeField").value = source.time_field || "";
  byID("parseRegex").value = source.regex_pattern || "";
  byID("parseFieldMapping").value = JSON.stringify(source.field_mapping || {}, null, 2);
  byID("parseTypeMapping").value = JSON.stringify(source.type_mapping || {}, null, 2);
}
function parseJSONMap(id) {
  const text = byID(id).value.trim();
  if (!text) return {};
  const parsed = JSON.parse(text);
  Object.keys(parsed).forEach(function(key) { parsed[key] = String(parsed[key]); });
  return parsed;
}
async function saveParsingConfig() {
  try {
    const id = byID("parseSource").value;
    const source = Object.assign({}, state.sources.find(item => item.id === id));
    if (!source.id) throw new Error("请选择数据源");
    source.parser = byID("parseParser").value;
    source.time_field = byID("parseTimeField").value.trim();
    source.regex_pattern = byID("parseRegex").value;
    source.field_mapping = parseJSONMap("parseFieldMapping");
    source.type_mapping = parseJSONMap("parseTypeMapping");
    const result = await fetchJSON("/api/v1/datasources", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(source)});
    setMessage("parseMessage", "Saved field settings: " + result.id, "ok");
    await loadDataSources();
    await previewRuntimePipelines();
  } catch (err) {
    setMessage("parseMessage", err.message, "bad");
  }
}
async function previewRuntimePipelines() {
  try {
    const result = await fetchJSON("/api/v1/runtime/pipelines");
    byID("pipelinePreview").textContent = JSON.stringify(result, null, 2);
  } catch (err) {
    byID("pipelinePreview").textContent = err.message;
  }
}
async function loadIndexes() {
  try {
    const result = await fetchJSON("/api/v1/indexes");
    state.indexes = result.indexes || [];
    byID("indexCount").textContent = state.indexes.length;
    renderIndexes();
  } catch (err) {
    const target = byID("indexesView");
    if (target) target.innerHTML = '<div class="empty">' + escapeHTML(err.message) + '</div>';
  }
}
function renderIndexes() {
  if (!byID("indexesView")) return;
  renderTable("indexesView", state.indexes, [
    {label: "Index", value: row => row.index_name, mono: true},
    {label: "Display name", value: row => row.name || "-"},
    {label: "Table", value: row => row.table_name || "-"},
    {label: "Rows", value: row => row.rows},
    {label: "Latest event", value: row => row.latest_event_time || "-"},
    {label: "TTL", value: row => row.ttl_days},
    {label: "Storage", value: row => row.storage},
    {label: "Status", value: row => row.status || "active"},
    {label: "Actions", html: true, value: row => '<button class="secondary" onclick="fillIndex(\'' + encodeURIComponent(row.index_name) + '\')">Edit</button> <button class="danger" onclick="deleteIndexConfig(\'' + encodeURIComponent(row.index_name) + '\')">Delete</button>'}
  ], "No indexes found");
}
function fillIndex(encodedName) {
  const name = decodeURIComponent(encodedName);
  const item = state.indexes.find(row => row.index_name === name);
  if (!item) return;
  byID("indexName").value = item.index_name;
  byID("indexDisplayName").value = item.name || item.index_name;
  byID("indexTTL").value = item.ttl_days || 30;
  byID("indexStatus").value = item.status || "active";
}
async function saveIndexConfig() {
  try {
    const body = {
      index_name: byID("indexName").value.trim(),
      name: byID("indexDisplayName").value.trim(),
      ttl_days: Number(byID("indexTTL").value),
      status: byID("indexStatus").value
    };
    const result = await fetchJSON("/api/v1/indexes", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(body)});
    setMessage("indexMessage", "Saved index: " + result.index_name, "ok");
    await loadIndexes();
  } catch (err) {
    setMessage("indexMessage", err.message, "bad");
  }
}
async function deleteIndexConfig(encodedName) {
  const name = decodeURIComponent(encodedName);
  const dropStorage = byID("indexDropStorage").value === "true";
  const text = dropStorage ? "确认删除 Index 配置并删除 ClickHouse 物理表 events_" + name + "？该操作会删除表内数据。" : "确认删除 Index 配置 " + name + "？";
  if (!confirm(text)) return;
  try {
    const url = "/api/v1/indexes?index=" + encodeURIComponent(name) + "&drop_storage=" + encodeURIComponent(String(dropStorage));
    await fetchJSON(url, {method: "DELETE"});
    setMessage("indexMessage", "Deleted index: " + name, "ok");
    await loadIndexes();
  } catch (err) {
    setMessage("indexMessage", err.message, "bad");
  }
}
function localToISO(value) {
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "" : date.toISOString();
}
function toLocalInputValue(date) {
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60000);
  return local.toISOString().slice(0, 16);
}
function setRangeMinutes(minutes) {
  const end = new Date();
  const start = new Date(end.getTime() - minutes * 60 * 1000);
  byID("startTime").value = toLocalInputValue(start);
  byID("endTime").value = toLocalInputValue(end);
}
function setRangeHours(hours) { setRangeMinutes(hours * 60); }
function clearRange() { byID("startTime").value = ""; byID("endTime").value = ""; }
function applyRangePreset() {
  const preset = byID("rangePreset") ? byID("rangePreset").value : "24h";
  if (preset === "15m") setRangeMinutes(15);
  if (preset === "24h") setRangeHours(24);
  if (preset === "7d") setRangeHours(24 * 7);
  if (preset === "all") clearRange();
}
function searchParams() {
  const params = new URLSearchParams();
  params.set("q", byID("spl").value);
  params.set("limit", byID("limit").value || "100");
  params.set("page", byID("page").value || "1");
  const start = localToISO(byID("startTime").value);
  const end = localToISO(byID("endTime").value);
  if (start) params.set("start_time", start);
  if (end) params.set("end_time", end);
  return params;
}
async function runSPL() {
  try {
    const result = await fetchJSON("/api/v1/search?" + searchParams().toString());
    state.currentResult = result;
    if (result.stats) {
      state.currentRows = result.stats.rows || [];
      renderStats(result.stats);
      byID("resultCount").textContent = state.currentRows.length;
      setMessage("searchMessage", "Statistics returned " + state.currentRows.length + " rows.", "ok");
      return;
    }
    state.currentRows = result.events || [];
    renderEvents(result.events || [], result.pagination);
    byID("resultCount").textContent = state.currentRows.length;
    setMessage("searchMessage", "Search returned " + state.currentRows.length + " events.", "ok");
    loadFields();
  } catch (err) {
    setMessage("searchMessage", err.message, "bad");
  }
}
function renderEvents(events, pagination) {
  renderTable("eventsView", events || [], [
    {label: "Time", value: e => e.event_time || ""},
    {label: "Index", value: e => e.metadata && e.metadata.index ? e.metadata.index : ""},
    {label: "Source", value: e => e.source ? e.source.type + "/" + e.source.name : ""},
    {label: "Service", value: e => e.fields && e.fields.service ? e.fields.service : ""},
    {label: "Fields", value: e => JSON.stringify(e.fields || {}), mono: true},
    {label: "Raw", value: e => e.raw || "", mono: true},
    {label: "Event ID", value: e => e.event_id || "", mono: true}
  ], "No matching events");
  if (pagination) {
    byID("eventsView").insertAdjacentHTML("afterbegin", '<div class="hint">Page ' + pagination.page + ', returned ' + pagination.returned + ', has_more=' + pagination.has_more + '</div>');
  }
  showTab("events");
}
function renderStats(stats) {
  const rows = stats && stats.rows ? stats.rows : [];
  const fields = stats && stats.fields && stats.fields.length ? stats.fields : (rows[0] ? Object.keys(rows[0]) : []);
  renderTable("statsView", rows, fields.map(field => ({label: field, value: row => row[field]})), "No statistics");
  showTab("stats");
}
function showTab(name) {
  ["events", "stats", "timeline", "fields", "raw"].forEach(function(item) {
    byID(item + "View").hidden = item !== name;
    byID("tab" + cap(item)).classList.toggle("active", item === name);
  });
}
function nextPage() {
  byID("page").value = String(Number(byID("page").value || "1") + 1);
  runSPL();
}
function prevPage() {
  byID("page").value = String(Math.max(1, Number(byID("page").value || "1") - 1));
  runSPL();
}
async function loadFields() {
  try {
    const params = searchParams();
    const result = await fetchJSON("/api/v1/search/fields?" + params.toString());
    const fields = result.fields || [];
    renderTable("fieldsView", fields, [
      {label: "Field", value: row => row.name},
      {label: "Type", value: row => row.type},
      {label: "Count", value: row => row.count},
      {label: "Samples", value: row => (row.samples || []).join(", "), mono: true}
    ], "No fields");
    renderFieldsRail(fields);
  } catch (err) {
    setMessage("searchMessage", err.message, "bad");
  }
}
function renderFieldsRail(fields) {
  const target = byID("searchFieldsRail");
  if (!target) return;
  if (!fields || !fields.length) {
    target.innerHTML = '<div class="empty">No fields</div>';
    return;
  }
  target.innerHTML = fields.map(function(field) {
    return '<div class="field-item" onclick="appendFieldToSearch(\'' + encodeURIComponent(field.name) + '\')"><strong>' + escapeHTML(field.name) + '</strong><span>' + escapeHTML(field.type || "") + " · " + escapeHTML(field.count || 0) + '</span></div>';
  }).join("");
}
function appendFieldToSearch(encoded) {
  const field = decodeURIComponent(encoded);
  const spl = byID("spl");
  spl.value = spl.value + " " + field + "=";
  spl.focus();
}
async function loadTimeline() {
  try {
    const params = searchParams();
    params.set("interval", "hour");
    const result = await fetchJSON("/api/v1/search/timeline?" + params.toString());
    const buckets = result.buckets || [];
    const max = Math.max(1, ...buckets.map(item => item.count));
    byID("timelineView").innerHTML = buckets.length ? buckets.map(item => {
      const width = Math.max(2, Math.round(item.count / max * 100));
      return '<div class="bar"><span class="mono">' + escapeHTML(item.start) + '</span><div class="bar-track"><div class="bar-fill" style="width:' + width + '%"></div></div><strong>' + item.count + '</strong></div>';
    }).join("") : '<div class="empty">No timeline data</div>';
    showTab("timeline");
  } catch (err) {
    setMessage("searchMessage", err.message, "bad");
  }
}

renderEvents([]);
renderStats(null);
init();
</script>
</body>
</html>`))
}
