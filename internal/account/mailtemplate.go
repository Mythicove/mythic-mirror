package account

import (
	"bytes"
	"html/template"
)

// mailTmpl 是验证/重置邮件的 HTML 模板（内联样式，兼容主流客户端）。
var mailTmpl = template.Must(template.New("mail").Parse(`<!doctype html>
<html>
<body style="margin:0;background:#f4f5f7;padding:24px;font-family:system-ui,sans-serif">
<div style="max-width:480px;margin:0 auto;background:#ffffff;border-radius:12px;overflow:hidden">
  <div style="background:#4f46e5;padding:20px 24px;color:#ffffff;font-size:18px;font-weight:600">{{.Subject}}</div>
  <div style="padding:24px;color:#1f2330;font-size:14px;line-height:1.6">
    <p>{{.Intro}}</p>
    <p style="margin:24px 0">
      <a href="{{.URL}}" style="background:#4f46e5;color:#ffffff;text-decoration:none;padding:10px 20px;border-radius:8px;display:inline-block">{{.Button}}</a>
    </p>
    <p style="color:#6b7280;font-size:12px;word-break:break-all">{{.URL}}</p>
    <p style="color:#6b7280;font-size:12px">{{.Footer}}</p>
  </div>
</div>
</body>
</html>`))

type mailData struct {
	Subject string
	Intro   string
	URL     string
	Button  string
	Footer  string
}

// renderMailHTML 渲染 HTML 邮件；失败返回空串（发送方退回纯文本）。
func renderMailHTML(d mailData) string {
	var buf bytes.Buffer
	if err := mailTmpl.Execute(&buf, d); err != nil {
		return ""
	}
	return buf.String()
}
