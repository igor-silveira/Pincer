package gateway

import "net/http"

func (g *Gateway) handleWebChatPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(webChatHTML))
}

const webChatHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Pincer</title>
<style>
:root {
  --bg-root: #0a0a0a;
  --bg-surface: #111111;
  --bg-elevated: #161616;
  --bg-input: #1a1a1a;
  --border: #222222;
  --border-subtle: #1c1c1c;
  --border-focus: #3b82f6;
  --text-primary: #d4d4d4;
  --text-secondary: #737373;
  --text-muted: #525252;
  --text-bright: #fafafa;
  --accent: #3b82f6;
  --accent-hover: #2563eb;
  --green: #22c55e;
  --green-dim: #166534;
  --red: #ef4444;
  --red-dim: #7f1d1d;
  --yellow: #eab308;
  --mono: "SF Mono", "Cascadia Code", "Fira Code", Consolas, "Liberation Mono", Menlo, monospace;
  --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
  --radius: 6px;
  --radius-lg: 10px;
}

* { box-sizing: border-box; margin: 0; padding: 0; }

body {
  font-family: var(--sans);
  background: var(--bg-root);
  color: var(--text-primary);
  height: 100vh;
  display: flex;
  flex-direction: column;
  font-size: 14px;
  line-height: 1.6;
  -webkit-font-smoothing: antialiased;
}

header {
  padding: 12px 20px;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}

header h1 {
  font-family: var(--mono);
  font-size: 13px;
  font-weight: 600;
  color: var(--text-bright);
  letter-spacing: 0.02em;
}

.status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--red);
  transition: background 0.3s ease;
  flex-shrink: 0;
}

.status-dot.connected { background: var(--green); }

.session-id {
  font-family: var(--mono);
  font-size: 11px;
  color: var(--text-muted);
  margin-left: auto;
}

#messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  scroll-behavior: smooth;
}

#messages::-webkit-scrollbar { width: 6px; }
#messages::-webkit-scrollbar-track { background: transparent; }
#messages::-webkit-scrollbar-thumb { background: #2a2a2a; border-radius: 3px; }
#messages::-webkit-scrollbar-thumb:hover { background: #3a3a3a; }

.msg-user {
  align-self: flex-end;
  max-width: 72%;
  padding: 10px 14px;
  background: var(--accent);
  color: #fff;
  border-radius: var(--radius-lg);
  border-bottom-right-radius: 3px;
  font-size: 14px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
  animation: msgIn 0.15s ease-out;
}

.msg-error {
  align-self: center;
  padding: 8px 16px;
  background: var(--red-dim);
  color: #fca5a5;
  border-radius: var(--radius);
  font-family: var(--mono);
  font-size: 12px;
  border: 1px solid #991b1b;
  animation: msgIn 0.15s ease-out;
}

.turn {
  align-self: flex-start;
  width: 100%;
  max-width: 85%;
  display: flex;
  flex-direction: column;
  gap: 0;
  border-left: 2px solid var(--border);
  padding-left: 14px;
  animation: turnIn 0.2s ease-out;
}

.turn.active { border-left-color: var(--accent); }

.text-block {
  color: var(--text-primary);
  line-height: 1.65;
  white-space: pre-wrap;
  word-break: break-word;
  padding: 2px 0;
}

.text-block p { margin: 0 0 8px 0; }
.text-block p:last-child { margin-bottom: 0; }
.text-block strong { color: var(--text-bright); font-weight: 600; }
.text-block em { color: var(--text-secondary); font-style: italic; }

.text-block a {
  color: var(--accent);
  text-decoration: none;
  border-bottom: 1px solid transparent;
  transition: border-color 0.15s;
}
.text-block a:hover { border-bottom-color: var(--accent); }

.text-block code {
  font-family: var(--mono);
  font-size: 12.5px;
  background: var(--bg-elevated);
  color: #e5c07b;
  padding: 2px 5px;
  border-radius: 3px;
  border: 1px solid var(--border);
}

.text-block pre {
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 12px;
  margin: 8px 0;
  overflow-x: auto;
  font-size: 12.5px;
  line-height: 1.5;
}
.text-block pre code {
  background: none;
  border: none;
  padding: 0;
  color: var(--text-primary);
  font-size: inherit;
}

.tool-card {
  margin: 6px 0;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  overflow: hidden;
  transition: border-color 0.2s;
}

.tool-card.running { border-color: var(--border); }
.tool-card.done { border-color: var(--green-dim); }

.tool-card-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  cursor: pointer;
  user-select: none;
  font-family: var(--mono);
  font-size: 12px;
  transition: background 0.1s;
}
.tool-card-header:hover { background: #1e1e1e; }

.tool-card-status {
  width: 16px;
  height: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.spinner {
  width: 12px;
  height: 12px;
  border: 1.5px solid var(--border);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: spin 0.7s linear infinite;
}

.checkmark {
  color: var(--green);
  font-size: 13px;
  line-height: 1;
}

.tool-card-name {
  color: var(--text-bright);
  font-weight: 500;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tool-card-chevron {
  color: var(--text-muted);
  font-size: 10px;
  transition: transform 0.15s ease;
  flex-shrink: 0;
}

.tool-card.expanded .tool-card-chevron { transform: rotate(90deg); }

.tool-card-body {
  display: none;
  border-top: 1px solid var(--border);
  padding: 10px;
  font-family: var(--mono);
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--text-secondary);
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 240px;
  overflow-y: auto;
}

.tool-card.expanded .tool-card-body { display: block; }

.approval-card {
  margin: 6px 0;
  background: var(--bg-elevated);
  border: 1px solid #854d0e;
  border-radius: var(--radius);
  overflow: hidden;
  animation: pulse-border 2s ease-in-out infinite;
}

.approval-card.resolved {
  border-color: var(--border);
  animation: none;
}

.approval-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  font-family: var(--mono);
  font-size: 12px;
}

.approval-icon {
  color: var(--yellow);
  font-size: 13px;
  flex-shrink: 0;
}

.approval-card.resolved .approval-icon { color: var(--text-muted); }

.approval-name {
  color: var(--text-bright);
  font-weight: 500;
  flex: 1;
}

.approval-card.resolved .approval-name { color: var(--text-secondary); }

.approval-input {
  border-top: 1px solid var(--border);
  padding: 10px;
  font-family: var(--mono);
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--text-secondary);
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 160px;
  overflow-y: auto;
}

.approval-actions {
  display: flex;
  gap: 8px;
  padding: 8px 10px;
  border-top: 1px solid var(--border);
}

.approval-btn {
  padding: 5px 14px;
  border: 1px solid;
  border-radius: 4px;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s;
  letter-spacing: 0.02em;
}

.approval-btn.approve {
  background: transparent;
  border-color: var(--green-dim);
  color: var(--green);
}
.approval-btn.approve:hover {
  background: var(--green-dim);
  color: var(--text-bright);
}

.approval-btn.deny {
  background: transparent;
  border-color: var(--red-dim);
  color: var(--red);
}
.approval-btn.deny:hover {
  background: var(--red-dim);
  color: var(--text-bright);
}

.approval-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}
.approval-btn:disabled:hover {
  background: transparent;
  color: inherit;
}

.approval-result {
  padding: 0 10px 8px;
  font-family: var(--mono);
  font-size: 11px;
  color: var(--text-muted);
}

.progress-line {
  font-family: var(--mono);
  font-size: 11.5px;
  color: var(--text-muted);
  padding: 3px 0;
  display: flex;
  align-items: center;
  gap: 6px;
}

.progress-line::before {
  content: "\203A";
  color: var(--text-muted);
  font-size: 14px;
  line-height: 1;
}

#input-area {
  padding: 12px 20px;
  background: var(--bg-surface);
  border-top: 1px solid var(--border);
  display: flex;
  gap: 10px;
  flex-shrink: 0;
}

#input {
  flex: 1;
  padding: 10px 14px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--bg-input);
  color: var(--text-primary);
  font-family: var(--sans);
  font-size: 14px;
  outline: none;
  transition: border-color 0.15s;
}

#input:focus { border-color: var(--border-focus); }

#input::placeholder { color: var(--text-muted); }

#input:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

#send {
  padding: 10px 18px;
  border: none;
  border-radius: var(--radius);
  background: var(--accent);
  color: #fff;
  font-family: var(--mono);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.15s;
  letter-spacing: 0.02em;
}

#send:hover { background: var(--accent-hover); }

#send:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

@keyframes msgIn {
  from { opacity: 0; transform: translateY(4px); }
  to { opacity: 1; transform: translateY(0); }
}

@keyframes turnIn {
  from { opacity: 0; transform: translateY(6px); }
  to { opacity: 1; transform: translateY(0); }
}

@keyframes pulse-border {
  0%, 100% { border-color: #854d0e; }
  50% { border-color: #a16207; }
}
</style>
</head>
<body>

<header>
  <div class="status-dot" id="status-dot"></div>
  <h1>pincer</h1>
  <span class="session-id" id="session-label"></span>
</header>

<div id="messages"></div>

<div id="input-area">
  <input type="text" id="input" placeholder="Send a message..." autocomplete="off" />
  <button id="send">Send</button>
</div>

<script>
(function() {
  var BT = String.fromCharCode(96);
  var BT3 = BT + BT + BT;

  var messagesEl = document.getElementById("messages");
  var inputEl = document.getElementById("input");
  var sendBtn = document.getElementById("send");
  var statusDot = document.getElementById("status-dot");
  var sessionLabel = document.getElementById("session-label");

  var ws = null;
  var sessionId = null;
  var currentTurn = null;
  var currentTextBlock = null;
  var rawText = "";
  var pendingToolCards = [];

  function scrollBottom() {
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function renderMarkdown(text) {
    var html = text;
    var blocks = [];
    var idx = 0;
    html = html.replace(new RegExp(BT3 + "([\\s\\S]*?)" + BT3, "g"), function(_, code) {
      var ph = "\x00BLOCK" + idx + "\x00";
      var lang = "";
      var src = code;
      var nl = code.indexOf("\n");
      if (nl !== -1 && nl < 20) {
        var first = code.substring(0, nl).trim();
        if (first && /^[a-zA-Z0-9_+-]+$/.test(first)) {
          lang = first;
          src = code.substring(nl + 1);
        }
      }
      blocks.push("<pre><code>" + escHtml(src.replace(/^\n|\n$/g, "")) + "</code></pre>");
      idx++;
      return ph;
    });

    html = html.replace(new RegExp(BT + "([^" + BT + "\\n]+)" + BT, "g"), function(_, code) {
      return "<code>" + escHtml(code) + "</code>";
    });

    html = escHtml(html);

    html = html.replace(/&lt;code&gt;/g, "<code>").replace(/&lt;\/code&gt;/g, "</code>");
    html = html.replace(/&lt;pre&gt;&lt;code&gt;/g, "<pre><code>").replace(/&lt;\/code&gt;&lt;\/pre&gt;/g, "</code></pre>");

    html = html.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
    html = html.replace(/\*([^*]+)\*/g, "<em>$1</em>");
    html = html.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');

    for (var i = 0; i < blocks.length; i++) {
      html = html.replace("\x00BLOCK" + i + "\x00", blocks[i]);
    }

    return html;
  }

  function escHtml(s) {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  }

  function prettyJson(s) {
    try {
      return JSON.stringify(JSON.parse(s), null, 2);
    } catch(e) {
      return s;
    }
  }

  function ensureTurn() {
    if (!currentTurn) {
      currentTurn = document.createElement("div");
      currentTurn.className = "turn active";
      messagesEl.appendChild(currentTurn);
    }
    return currentTurn;
  }

  function ensureTextBlock() {
    if (!currentTextBlock) {
      currentTextBlock = document.createElement("div");
      currentTextBlock.className = "text-block";
      ensureTurn().appendChild(currentTextBlock);
    }
    return currentTextBlock;
  }

  function finalizeTextBlock() {
    if (currentTextBlock && rawText) {
      currentTextBlock.innerHTML = renderMarkdown(rawText);
    }
    currentTextBlock = null;
    rawText = "";
  }

  function createToolCard(toolName, toolInput) {
    finalizeTextBlock();

    var card = document.createElement("div");
    card.className = "tool-card running";

    var header = document.createElement("div");
    header.className = "tool-card-header";

    var status = document.createElement("div");
    status.className = "tool-card-status";
    status.innerHTML = '<div class="spinner"></div>';

    var name = document.createElement("span");
    name.className = "tool-card-name";
    name.textContent = toolName;

    var chevron = document.createElement("span");
    chevron.className = "tool-card-chevron";
    chevron.textContent = "\u25B8";

    header.appendChild(status);
    header.appendChild(name);
    header.appendChild(chevron);

    var body = document.createElement("div");
    body.className = "tool-card-body";
    body.textContent = prettyJson(toolInput);

    header.addEventListener("click", function() {
      card.classList.toggle("expanded");
    });

    card.appendChild(header);
    card.appendChild(body);
    ensureTurn().appendChild(card);
    return card;
  }

  function markToolCardDone(card) {
    if (!card) return;
    card.classList.remove("running");
    card.classList.add("done");
    var st = card.querySelector(".tool-card-status");
    if (st) st.innerHTML = '<span class="checkmark">\u2713</span>';
  }

  function createApprovalCard(requestId, toolName, toolInput) {
    finalizeTextBlock();

    var card = document.createElement("div");
    card.className = "approval-card";

    var header = document.createElement("div");
    header.className = "approval-header";

    var icon = document.createElement("span");
    icon.className = "approval-icon";
    icon.textContent = "\u26A0";

    var name = document.createElement("span");
    name.className = "approval-name";
    name.textContent = toolName;

    header.appendChild(icon);
    header.appendChild(name);

    var inputArea = document.createElement("div");
    inputArea.className = "approval-input";
    inputArea.textContent = prettyJson(toolInput);

    var actions = document.createElement("div");
    actions.className = "approval-actions";

    var approveBtn = document.createElement("button");
    approveBtn.className = "approval-btn approve";
    approveBtn.textContent = "Approve";

    var denyBtn = document.createElement("button");
    denyBtn.className = "approval-btn deny";
    denyBtn.textContent = "Deny";

    function respond(approved) {
      approveBtn.disabled = true;
      denyBtn.disabled = true;
      card.classList.add("resolved");

      var result = document.createElement("div");
      result.className = "approval-result";
      result.textContent = approved ? "\u2713 Approved" : "\u2717 Denied";
      card.appendChild(result);

      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({
          type: "approval_response",
          request_id: requestId,
          approved: approved
        }));
      }
      scrollBottom();
    }

    approveBtn.addEventListener("click", function() { respond(true); });
    denyBtn.addEventListener("click", function() { respond(false); });

    actions.appendChild(approveBtn);
    actions.appendChild(denyBtn);

    card.appendChild(header);
    card.appendChild(inputArea);
    card.appendChild(actions);
    ensureTurn().appendChild(card);
    return card;
  }

  function addProgressLine(text) {
    var line = document.createElement("div");
    line.className = "progress-line";
    line.textContent = text;
    ensureTurn().appendChild(line);
    scrollBottom();
  }

  function addUserMessage(text) {
    var el = document.createElement("div");
    el.className = "msg-user";
    el.textContent = text;
    messagesEl.appendChild(el);
    scrollBottom();
  }

  function addErrorMessage(text) {
    var el = document.createElement("div");
    el.className = "msg-error";
    el.textContent = text;
    messagesEl.appendChild(el);
    scrollBottom();
  }

  function resetTurnState() {
    finalizeTextBlock();
    for (var i = 0; i < pendingToolCards.length; i++) {
      markToolCardDone(pendingToolCards[i]);
    }
    pendingToolCards = [];
    if (currentTurn) currentTurn.classList.remove("active");
    currentTurn = null;
    currentTextBlock = null;
    rawText = "";
  }

  function setInputEnabled(enabled) {
    sendBtn.disabled = !enabled;
    inputEl.disabled = !enabled;
    if (enabled) inputEl.focus();
  }

  function connect() {
    var proto = location.protocol === "https:" ? "wss:" : "ws:";
    ws = new WebSocket(proto + "//" + location.host + "/ws");

    ws.onopen = function() {
      statusDot.classList.add("connected");
    };

    ws.onclose = function() {
      statusDot.classList.remove("connected");
      setTimeout(connect, 2000);
    };

    ws.onerror = function() {
      statusDot.classList.remove("connected");
    };

    ws.onmessage = function(e) {
      var msg = JSON.parse(e.data);

      switch (msg.type) {
        case "session":
          sessionId = msg.session_id;
          sessionLabel.textContent = msg.session_id.substring(0, 8);
          break;

        case "token":
          ensureTextBlock();
          rawText += msg.content;
          currentTextBlock.innerHTML = renderMarkdown(rawText);
          scrollBottom();
          break;

        case "tool_call":
          var card = createToolCard(msg.tool_name, msg.tool_input);
          pendingToolCards.push(card);
          scrollBottom();
          break;

        case "tool_result":
          if (pendingToolCards.length > 0) {
            markToolCardDone(pendingToolCards.shift());
          }
          scrollBottom();
          break;

        case "approval_request":
          createApprovalCard(msg.request_id, msg.tool_name, msg.tool_input);
          scrollBottom();
          break;

        case "progress":
          addProgressLine(msg.content);
          break;

        case "message":
          resetTurnState();
          var msgTurn = document.createElement("div");
          msgTurn.className = "turn";
          var msgBlock = document.createElement("div");
          msgBlock.className = "text-block";
          msgBlock.innerHTML = renderMarkdown(msg.content);
          msgTurn.appendChild(msgBlock);
          messagesEl.appendChild(msgTurn);
          scrollBottom();
          break;

        case "done":
          resetTurnState();
          setInputEnabled(true);
          break;

        case "error":
          resetTurnState();
          addErrorMessage(msg.error);
          setInputEnabled(true);
          break;
      }
    };
  }

  function send() {
    var text = inputEl.value.trim();
    if (!text || !ws || ws.readyState !== WebSocket.OPEN) return;
    addUserMessage(text);
    ws.send(JSON.stringify({ type: "message", content: text }));
    inputEl.value = "";
    setInputEnabled(false);
  }

  sendBtn.addEventListener("click", send);
  inputEl.addEventListener("keydown", function(e) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  });

  connect();
})();
</script>
</body>
</html>`
