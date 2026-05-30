package version

const (
	Name    = "mcp-language-server-hakantr"
	Version = "0.1.0-hakantr.1"
)

func String() string {
	return Name + " " + Version
}
