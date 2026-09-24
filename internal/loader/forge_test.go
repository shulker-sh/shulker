package loader

import (
	"context"
	"testing"
)

func TestForgeVersionsAreFilteredByGame(t *testing.T) {
	r := fakeRemote(t, ForgeMavenURL, map[string]any{
		"/net/minecraftforge/forge/maven-metadata.xml": `<metadata><versioning><versions>
			<version>26.1-64.0.1</version><version>26.2-65.1.3</version><version>26.2-65.1.4</version>
		</versions></versioning></metadata>`,
	})
	got, err := forge.Versions(context.Background(), r, "26.2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Version{"65.1.3", true}) || got[1] != (Version{"65.1.4", true}) {
		t.Fatalf("versions %v", got)
	}
	if url := forgeInstallerURL(r, "26.2", "65.1.3"); url != r.url(ForgeMavenURL)+"/net/minecraftforge/forge/26.2-65.1.3/forge-26.2-65.1.3-installer.jar" {
		t.Fatalf("installer url %s", url)
	}
}
