package loader

import "testing"

func TestMavenPath(t *testing.T) {
	cases := map[string]string{
		"org.ow2.asm:asm:9.3":                                    "org/ow2/asm/asm/9.3/asm-9.3.jar",
		"net.minecraftforge:forge:1.16.5-36.2.42:universal":      "net/minecraftforge/forge/1.16.5-36.2.42/forge-1.16.5-36.2.42-universal.jar",
		"de.oceanlabs.mcp:mcp_config:1.16.5-20210115.111550@zip": "de/oceanlabs/mcp/mcp_config/1.16.5-20210115.111550/mcp_config-1.16.5-20210115.111550.zip",
	}
	for name, want := range cases {
		if got, err := MavenPath(name); err != nil || got != want {
			t.Errorf("%s: got %q, %v", name, got, err)
		}
	}
}

func TestMavenPathRefusesAPathInACoordinate(t *testing.T) {
	for _, name := range []string{
		"a:b:1@x/../../../tmp/pwn.sh",
		"a:../..:1",
		"a:b:../../x",
		"..:b:1",
		"/etc:b:1",
		`a:b\c:1`,
		"a:b",
		"a:b:1:c:d",
	} {
		if got, err := MavenPath(name); err == nil {
			t.Errorf("%s: got %q, want an error", name, got)
		}
	}
}
