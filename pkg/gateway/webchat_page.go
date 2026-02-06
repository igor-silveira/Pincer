package gateway

import "net/http"

func (g *Gateway) handleWebChatPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(webChatHTML))
}

const webChatHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Pincer WebChat</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #0f0f0f; color: #e0e0e0; height: 100vh; display: flex; flex-direction: column; }
  header { padding: 16px 24px; background: #1a1a1a; border-bottom: 1px solid #333; }
  header h1 { font-size: 18px; font-weight: 600; }
  #messages { flex: 1; overflow-y: auto; padding: 24px; display: flex; flex-direction: column; gap: 12px; }
  .msg { max-width: 70%; padding: 12px 16px; border-radius: 12px; line-height: 1.5; white-space: pre-wrap; word-wrap: break-word; }
  .msg.user { align-self: flex-end; background: #2563eb; color: #fff; }
  .msg.assistant { align-self: flex-start; background: #262626; }
  .msg.error { align-self: center; background: #991b1b; color: #fca5a5; font-size: 14px; }
  #input-area { padding: 16px 24px; background: #1a1a1a; border-top: 1px solid #333; display: flex; gap: 12px; }
  #input { flex: 1; padding: 12px 16px; border: 1px solid #444; border-radius: 8px; background: #262626; color: #e0e0e0; font-size: 15px; outline: none; }
  #input:focus { border-color: #2563eb; }
  #send { padding: 12px 24px; border: none; border-radius: 8px; background: #2563eb; color: #fff; font-size: 15px; cursor: pointer; }
  #send:hover { background: #1d4ed8; }
  #send:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
</head>
<body>
<header><h1>Pincer WebChat</h1></header>
<div id="messages"></div>
<div id="input-area">
  <input type="text" id="input" placeholder="Type a message..." autocomplete="off" />
  <button id="send">Send</button>
</div>
<script>
(function() {
  const messagesEl = document.getElementById('messages');
  const inputEl = document.getElementById('input');
  const sendBtn = document.getElementById('send');
  let ws, sessionId, currentAssistantEl;

  function connect() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(proto + '//' + location.host + '/ws');
    ws.onopen = () => console.log('connected');
    ws.onclose = () => setTimeout(connect, 2000);
    ws.onmessage = (e) => {
      const msg = JSON.parse(e.data);
      switch (msg.type) {
        case 'session':
          sessionId = msg.session_id;
          break;
        case 'token':
          if (!currentAssistantEl) {
            currentAssistantEl = addMessage('', 'assistant');
          }
          currentAssistantEl.textContent += msg.content;
          messagesEl.scrollTop = messagesEl.scrollHeight;
          break;
        case 'done':
          currentAssistantEl = null;
          sendBtn.disabled = false;
          inputEl.disabled = false;
          inputEl.focus();
          break;
        case 'error':
          addMessage(msg.error, 'error');
          sendBtn.disabled = false;
          inputEl.disabled = false;
          break;
      }
    };
  }

  function addMessage(text, role) {
    const el = document.createElement('div');
    el.className = 'msg ' + role;
    el.textContent = text;
    messagesEl.appendChild(el);
    messagesEl.scrollTop = messagesEl.scrollHeight;
    return el;
  }

  function send() {
    const text = inputEl.value.trim();
    if (!text || !ws || ws.readyState !== WebSocket.OPEN) return;
    addMessage(text, 'user');
    ws.send(JSON.stringify({ type: 'message', content: text }));
    inputEl.value = '';
    sendBtn.disabled = true;
    inputEl.disabled = true;
  }

  sendBtn.addEventListener('click', send);
  inputEl.addEventListener('keydown', (e) => { if (e.key === 'Enter') send(); });
  connect();
})();
</script>
</body>
</html>`
