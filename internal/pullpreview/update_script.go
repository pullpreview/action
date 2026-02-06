package pullpreview

import (
	"bytes"
	"embed"
	"strings"
	"text/template"
)

//go:embed templates/update_script.sh.tmpl
var updateScriptFS embed.FS

var updateScriptTemplate = template.Must(template.New("update_script").Parse(loadUpdateScript()))

type UpdateScriptData struct {
	RemoteAppPath  string
	ComposeFiles   string
	ComposeOptions string
	PublicIP       string
	PublicDNS      string
	URL            string
}

func RenderUpdateScript(data UpdateScriptData) (string, error) {
	var buf bytes.Buffer
	if err := updateScriptTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func loadUpdateScript() string {
	content, err := updateScriptFS.ReadFile("templates/update_script.sh.tmpl")
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(string(content), "\r\n", "\n")
}
