import * as vscode from 'vscode';
import * as ws from 'ws';

interface MorpheusMessage {
  type: 'chat' | 'tool_result' | 'error' | 'status';
  content?: string;
  tool?: string;
  data?: any;
  error?: string;
  status?: 'connected' | 'disconnected' | 'error';
}

interface MorpheusConfig {
  endpoint: string;
  apiKey: string;
  autostart: boolean;
  maxTokens: number;
  temperature: number;
  undercover: boolean;
  forkIsolation: boolean;
}

class MorpheusChatProvider implements vscode.WebviewViewProvider {
  private webview?: vscode.WebviewView;
  private connection?: ws;
  private messages: MorpheusMessage[] = [];

  constructor(
    private context: vscode.ExtensionContext,
    private config: MorpheusConfig
  ) {}

  resolveWebviewView(webview: vscode.WebviewView): void {
    this.webview = webview;

    webview.webview.options = {
      enableScripts: true,
      localResourceRoots: [this.context.extensionUri]
    };

    webview.webview.html = this.getChatHTML();

    webview.webview.onDidReceiveMessage(async (message) => {
      if (message.type === 'chat') {
        await this.sendChat(message.content);
      } else if (message.type === 'configure') {
        this.updateConfig(message.config);
      }
    });
  }

  private getChatHTML(): string {
    return `
<!DOCTYPE html>
<html>
<head>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; height: 100vh; display: flex; flex-direction: column; }
    .header { background: #007acc; color: white; padding: 10px 15px; font-weight: 600; }
    .messages { flex: 1; overflow-y: auto; padding: 15px; }
    .message { margin-bottom: 10px; padding: 8px 12px; border-radius: 5px; max-width: 90%; }
    .message.user { background: #e3f2fd; margin-left: auto; }
    .message.assistant { background: #f5f5f5; }
    .message.error { background: #ffebee; color: #c62828; }
    .input-area { display: flex; padding: 10px; border-top: 1px solid #ddd; }
    .input-area input { flex: 1; padding: 8px 12px; border: 1px solid #ddd; border-radius: 4px; font-size: 14px; }
    .input-area button { margin-left: 10px; padding: 8px 16px; background: #007acc; color: white; border: none; border-radius: 4px; cursor: pointer; }
    .input-area button:hover { background: #005a9e; }
    .input-area button:disabled { background: #ccc; cursor: not-allowed; }
    .status { font-size: 12px; color: #666; padding: 5px 15px; }
    .status.connected { color: #2e7d32; }
    .status.error { color: #c62828; }
  </style>
</head>
<body>
  <div class="header">Morpheus AI Assistant</div>
  <div class="status" id="status">Disconnected</div>
  <div class="messages" id="messages"></div>
  <div class="input-area">
    <input type="text" id="input" placeholder="Ask Morpheus..." />
    <button id="send" onclick="sendMessage()">Send</button>
  </div>
  <script>
    const vscode = acquireVsCodeApi();
    let connected = false;

    function sendMessage() {
      const input = document.getElementById('input');
      const text = input.value.trim();
      if (!text) return;
      
      addMessage('user', text);
      input.value = '';
      
      vscode.postMessage({ type: 'chat', content: text });
    }

    function addMessage(role, content) {
      const messages = document.getElementById('messages');
      const div = document.createElement('div');
      div.className = 'message ' + role;
      div.textContent = content;
      messages.appendChild(div);
      messages.scrollTop = messages.scrollHeight;
    }

    function setStatus(status, text) {
      const statusEl = document.getElementById('status');
      statusEl.textContent = text;
      statusEl.className = 'status ' + status;
    }

    window.addEventListener('message', (event) => {
      const msg = event.data;
      if (msg.type === 'chat') {
        addMessage('assistant', msg.content || '');
      } else if (msg.type === 'error') {
        addMessage('error', msg.error || 'Unknown error');
      } else if (msg.type === 'status') {
        connected = msg.status === 'connected';
        setStatus(msg.status, msg.status === 'connected' ? 'Connected to Morpheus' : 'Disconnected');
      }
    });

    document.getElementById('input').addEventListener('keypress', (e) => {
      if (e.key === 'Enter') sendMessage();
    });
  </script>
</body>
</html>`;
  }

  private async sendChat(content: string): Promise<void> {
    if (!this.connection || this.connection.readyState !== ws.OPEN) {
      vscode.window.showErrorMessage('Morpheus is not connected. Please start the assistant first.');
      return;
    }

    try {
      this.connection.send(JSON.stringify({
        type: 'chat',
        content,
        config: {
          undercover: this.config.undercover,
          forkIsolation: this.config.forkIsolation
        }
      }));
    } catch (error) {
      vscode.window.showErrorMessage('Failed to send message: ' + String(error));
    }
  }

  private updateConfig(config: Partial<MorpheusConfig>): void {
    this.config = { ...this.config, ...config };
  }

  connect(): void {
    if (this.connection) {
      this.connection.close();
    }

    try {
      this.connection = new ws(this.config.endpoint, {
        headers: this.config.apiKey ? {
          'Authorization': `Bearer ${this.config.apiKey}`
        } : {}
      });

      this.connection.on('open', () => {
        this.webview?.webview.postMessage({ type: 'status', status: 'connected' });
      });

      this.connection.on('message', (data) => {
        try {
          const msg: MorpheusMessage = JSON.parse(data.toString());
          this.webview?.webview.postMessage(msg);
        } catch (error) {
          console.error('Failed to parse message:', error);
        }
      });

      this.connection.on('close', () => {
        this.webview?.webview.postMessage({ type: 'status', status: 'disconnected' });
      });

      this.connection.on('error', (error) => {
        this.webview?.webview.postMessage({ type: 'status', status: 'error' });
        console.error('WebSocket error:', error);
      });
    } catch (error) {
      vscode.window.showErrorMessage('Failed to connect to Morpheus: ' + String(error));
    }
  }

  disconnect(): void {
    if (this.connection) {
      this.connection.close();
      this.connection = undefined;
    }
  }

  async chat(prompt: string): Promise<string> {
    if (!this.connection || this.connection.readyState !== ws.OPEN) {
      throw new Error('Morpheus is not connected');
    }

    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error('Chat request timed out'));
      }, 60000);

      const handler = (data: ws.Data) => {
        try {
          const msg: MorpheusMessage = JSON.parse(data.toString());
          if (msg.type === 'chat') {
            clearTimeout(timeout);
            this.connection?.removeListener('message', handler);
            resolve(msg.content || '');
          } else if (msg.type === 'error') {
            clearTimeout(timeout);
            this.connection?.removeListener('message', handler);
            reject(new Error(msg.error || 'Unknown error'));
          }
        } catch (error) {
          console.error('Failed to parse message:', error);
        }
      };

      this.connection?.on('message', handler);
      this.connection?.send(JSON.stringify({ type: 'chat', content: prompt }));
    });
  }
}

export function activate(context: vscode.ExtensionContext) {
  const config = vscode.workspace.getConfiguration('morpheus');
  const chatProvider = new MorpheusChatProvider(context, {
    endpoint: config.get('endpoint', 'ws://localhost:8080'),
    apiKey: config.get('apiKey', ''),
    autostart: config.get('autostart', false),
    maxTokens: config.get('maxTokens', 4096),
    temperature: config.get('temperature', 0.4),
    undercover: config.get('undercover', false),
    forkIsolation: config.get('forkIsolation', false)
  });

  context.subscriptions.push(
    vscode.commands.registerCommand('morpheus.start', () => {
      chatProvider.connect();
      vscode.window.showInformationMessage('Morpheus AI Assistant started');
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand('morpheus.stop', () => {
      chatProvider.disconnect();
      vscode.window.showInformationMessage('Morpheus AI Assistant stopped');
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand('morpheus.chat', () => {
      vscode.window.showInformationMessage('Opening Morpheus chat...');
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand('morpheus.inline', async () => {
      const editor = vscode.window.activeTextEditor;
      if (!editor) {
        vscode.window.showWarningMessage('No active editor');
        return;
      }

      const selection = editor.selection;
      const selectedText = editor.document.getText(selection);

      if (!selectedText) {
        vscode.window.showWarningMessage('No text selected');
        return;
      }

      try {
        const result = await chatProvider.chat(`Refactor this code:\n${selectedText}`);
        editor.edit(editBuilder => {
          editBuilder.replace(selection, result);
        });
      } catch (error) {
        vscode.window.showErrorMessage('Failed to refactor: ' + String(error));
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand('morpheus.explain', async () => {
      const editor = vscode.window.activeTextEditor;
      if (!editor) {
        vscode.window.showWarningMessage('No active editor');
        return;
      }

      const selection = editor.selection;
      const selectedText = editor.document.getText(selection);

      if (!selectedText) {
        vscode.window.showWarningMessage('No text selected');
        return;
      }

      try {
        const result = await chatProvider.chat(`Explain this code:\n${selectedText}`);
        vscode.window.showInformationMessage(result);
      } catch (error) {
        vscode.window.showErrorMessage('Failed to explain: ' + String(error));
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand('morpheus.refactor', async () => {
      const editor = vscode.window.activeTextEditor;
      if (!editor) {
        vscode.window.showWarningMessage('No active editor');
        return;
      }

      const selection = editor.selection;
      const selectedText = editor.document.getText(selection);

      if (!selectedText) {
        vscode.window.showWarningMessage('No text selected');
        return;
      }

      try {
        const result = await chatProvider.chat(`Refactor this code:\n${selectedText}`);
        editor.edit(editBuilder => {
          editBuilder.replace(selection, result);
        });
      } catch (error) {
        vscode.window.showErrorMessage('Failed to refactor: ' + String(error));
      }
    })
  );

  const provider = vscode.window.registerWebviewViewProvider(
    'morpheus.chat.view',
    chatProvider
  );

  context.subscriptions.push(provider);

  if (config.get('autostart', false)) {
    chatProvider.connect();
  }
}

export function deactivate() {}
