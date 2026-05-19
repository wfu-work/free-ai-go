package services

import (
	"context"
	"errors"
	"html/template"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/wfu-work/nav-common-go-lib/global"
	"go.uber.org/zap"
)

var openAICallbackServerOnce sync.Once

type openAICallbackPageData struct {
	CallbackURL string
	Code        string
	State       string
	Scope       string
	Error       string
}

func StartOpenAIOAuthCallbackServer() {
	cfg := Config()
	if !cfg.OpenAICallbackEnabled {
		return
	}
	openAICallbackServerOnce.Do(func() {
		go runOpenAIOAuthCallbackServer(cfg.OpenAICallbackAddr)
	})
}

func runOpenAIOAuthCallbackServer(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", handleOpenAIOAuthCallback)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		if !errors.Is(err, http.ErrServerClosed) && global.NAV_LOG != nil {
			global.NAV_LOG.Warn("OpenAI OAuth callback server not started", zap.String("addr", addr), zap.Error(err))
		}
		return
	}
	if global.NAV_LOG != nil {
		global.NAV_LOG.Info("OpenAI OAuth callback server started", zap.String("addr", listener.Addr().String()))
	}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && global.NAV_LOG != nil {
		global.NAV_LOG.Warn("OpenAI OAuth callback server stopped", zap.String("addr", addr), zap.Error(err))
	}
	_ = server.Shutdown(context.Background())
}

func handleOpenAIOAuthCallback(w http.ResponseWriter, r *http.Request) {
	callbackURL := "http://" + r.Host + r.URL.RequestURI()
	data := openAICallbackPageData{
		CallbackURL: callbackURL,
		Code:        r.URL.Query().Get("code"),
		State:       r.URL.Query().Get("state"),
		Scope:       r.URL.Query().Get("scope"),
		Error:       r.URL.Query().Get("error"),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = openAICallbackTemplate.Execute(w, data)
}

var openAICallbackTemplate = template.Must(template.New("openai-callback").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>OpenAI 授权回调</title>
  <style>
    :root {
      color-scheme: light;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      color: #111827;
      background: #f6f8fb;
    }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
    }
    main {
      width: min(880px, calc(100vw - 48px));
      padding: 32px;
      border: 1px solid #dbe3ef;
      border-radius: 12px;
      background: #fff;
      box-shadow: 0 20px 60px rgba(15, 23, 42, .08);
    }
    h1 {
      margin: 0 0 12px;
      font-size: 28px;
    }
    p {
      margin: 0 0 18px;
      color: #4b5563;
      line-height: 1.7;
    }
    textarea {
      width: 100%;
      min-height: 132px;
      box-sizing: border-box;
      padding: 14px;
      border: 1px solid #cbd5e1;
      border-radius: 8px;
      font: 13px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      color: #111827;
      background: #f8fafc;
      resize: vertical;
    }
    .actions {
      display: flex;
      gap: 12px;
      margin-top: 18px;
    }
    button {
      cursor: pointer;
      height: 38px;
      padding: 0 18px;
      border: 1px solid #cbd5e1;
      border-radius: 999px;
      color: #111827;
      background: #fff;
      font-size: 14px;
    }
    button.primary {
      border-color: #2563eb;
      color: #fff;
      background: #2563eb;
    }
    .status {
      margin-top: 14px;
      color: #2563eb;
      font-size: 14px;
    }
    .error {
      color: #dc2626;
    }
  </style>
</head>
<body>
  <main>
    {{ if .Error }}
      <h1>OpenAI 授权失败</h1>
      <p class="error">OpenAI 返回错误：{{ .Error }}</p>
    {{ else }}
      <h1>OpenAI 授权回调已收到</h1>
      <p>完整回调 URL 已生成。系统会尝试自动回填到账号表单；如果没有自动回填，请复制下面的 URL 回到新增账号页面手动解析。</p>
    {{ end }}
    <textarea id="callbackUrl" readonly>{{ .CallbackURL }}</textarea>
    <div class="actions">
      <button class="primary" id="copyBtn" type="button">复制回调 URL</button>
      <button id="closeBtn" type="button">关闭页面</button>
    </div>
    <div id="status" class="status"></div>
  </main>
  <script>
    (function () {
      var callbackUrl = document.getElementById('callbackUrl').value;
      var payload = { type: 'freeai.openai.oauth.callback', callbackUrl: callbackUrl };
      var status = document.getElementById('status');
      try {
        if (window.opener && !window.opener.closed) {
          window.opener.postMessage(payload, '*');
          status.textContent = '已发送到账号表单，稍等片刻会自动解析。';
          setTimeout(function () { window.close(); }, 1200);
        } else {
          status.textContent = '未找到原账号表单窗口，请手动复制回调 URL。';
        }
      } catch (error) {
        status.textContent = '自动发送失败，请手动复制回调 URL。';
      }
      document.getElementById('copyBtn').addEventListener('click', function () {
        navigator.clipboard.writeText(callbackUrl).then(function () {
          status.textContent = '回调 URL 已复制。';
        }, function () {
          var input = document.getElementById('callbackUrl');
          input.focus();
          input.select();
          status.textContent = '请使用快捷键复制选中的回调 URL。';
        });
      });
      document.getElementById('closeBtn').addEventListener('click', function () {
        window.close();
      });
    })();
  </script>
</body>
</html>`))
