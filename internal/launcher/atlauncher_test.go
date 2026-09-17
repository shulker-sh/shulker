package launcher

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMergeVersion(t *testing.T) {
	vanilla := json.RawMessage(`{
		"id": "26.2", "mainClass": "net.minecraft.client.main.Main", "type": "release",
		"downloads": {"client": {"url": "https://piston-data/client.jar"}},
		"arguments": {"game": ["--username"], "jvm": ["-cp", "${classpath}"]},
		"libraries": [{"name": "com.mojang:brigadier:1.3.10", "downloads": {"artifact": {"path": "com/mojang/brigadier/1.3.10/brigadier-1.3.10.jar", "url": "https://libraries.minecraft.net/com/mojang/brigadier/1.3.10/brigadier-1.3.10.jar"}}}]
	}`)
	fabric := json.RawMessage(`{
		"id": "fabric-loader-0.17.3-26.2", "inheritsFrom": "26.2", "mainClass": "net.fabricmc.loader.impl.launch.knot.KnotClient",
		"arguments": {"game": [], "jvm": ["-DFabricMcEmu= net.minecraft.client.main.Main "]},
		"libraries": [
			{"name": "net.fabricmc:fabric-loader:0.17.3", "url": "https://maven.fabricmc.net/", "sha1": "abc", "size": 12},
			{"name": "org.lwjgl:lwjgl:3.3.3:natives-macos@zip", "url": "https://maven.example"}
		]
	}`)
	merged, err := MergeVersion(vanilla, fabric)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		ID           string         `json:"id"`
		InheritsFrom *string        `json:"inheritsFrom"`
		MainClass    string         `json:"mainClass"`
		Downloads    map[string]any `json:"downloads"`
		Arguments    struct {
			Game []string `json:"game"`
			JVM  []string `json:"jvm"`
		} `json:"arguments"`
		Libraries []struct {
			Name      string `json:"name"`
			URL       string `json:"url"`
			Downloads struct {
				Artifact map[string]any `json:"artifact"`
			} `json:"downloads"`
		} `json:"libraries"`
	}
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "26.2" || got.InheritsFrom != nil || got.MainClass != "net.fabricmc.loader.impl.launch.knot.KnotClient" || got.Downloads["client"] == nil {
		t.Fatalf("merged head: %s", merged)
	}
	if want := []string{"-cp", "${classpath}", "-DFabricMcEmu= net.minecraft.client.main.Main "}; !reflect.DeepEqual(got.Arguments.JVM, want) || !reflect.DeepEqual(got.Arguments.Game, []string{"--username"}) {
		t.Fatalf("arguments: %+v", got.Arguments)
	}
	if len(got.Libraries) != 3 || got.Libraries[2].Name != "com.mojang:brigadier:1.3.10" {
		t.Fatalf("libraries should be the loader's then vanilla's: %s", merged)
	}
	loaderLib := got.Libraries[0]
	if want := map[string]any{"path": "net/fabricmc/fabric-loader/0.17.3/fabric-loader-0.17.3.jar", "url": "https://maven.fabricmc.net/net/fabricmc/fabric-loader/0.17.3/fabric-loader-0.17.3.jar", "sha1": "abc", "size": float64(12)}; !reflect.DeepEqual(loaderLib.Downloads.Artifact, want) || loaderLib.URL != "" {
		t.Fatalf("converted library: %+v", loaderLib)
	}
	if path := got.Libraries[1].Downloads.Artifact["path"]; path != "org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3-natives-macos.zip" {
		t.Fatalf("classifier and extension: %v", path)
	}
}

func TestATLauncherWriteInstanceKeepsSettings(t *testing.T) {
	l := &ATLauncher{Dir: t.TempDir()}
	inst := ATLauncherInstance{Name: "Friends SMP!", Minecraft: "26.2", LoaderType: "neoforge", LoaderVersion: "26.2.0.87", Version: json.RawMessage(`{"id":"26.2"}`)}
	res, err := l.WriteInstance(inst)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(l.Dir, "instances", "FriendsSMP"); res.Dir != want || res.GameDir != want || !res.Created {
		t.Fatalf("result: %+v", res)
	}
	path := filepath.Join(res.Dir, ATLauncherInstanceFile)
	first := readInstanceJSON(t, path)
	first["launcher"].(map[string]any)["maximumMemory"] = 8192
	data, _ := json.Marshal(first)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if res, err = l.WriteInstance(inst); err != nil || res.Created {
		t.Fatalf("relink: %+v %v", res, err)
	}
	again := readInstanceJSON(t, path)
	settings := again["launcher"].(map[string]any)
	// enableCommands belongs to the slots now, so reconcile sets it, not WriteInstance.
	if again["uuid"] != first["uuid"] || settings["maximumMemory"] != float64(8192) {
		t.Fatalf("relink should keep uuid and player settings: %v", again)
	}
	if settings["requiredMemory"] != float64(0) || settings["requiredPermGen"] != float64(0) {
		t.Fatalf("ATLauncher unboxes requiredMemory and requiredPermGen on launch: %v", settings)
	}
	if lv := settings["loaderVersion"].(map[string]any); lv["type"] != "NeoForge" || lv["version"] != "26.2.0.87" {
		t.Fatalf("loaderVersion: %v", lv)
	}
}

func TestATLauncherWriteInstanceKeepsPlayerImage(t *testing.T) {
	l := &ATLauncher{Dir: t.TempDir()}
	inst := ATLauncherInstance{Name: "Pack", Minecraft: "26.2", LoaderType: "fabric", LoaderVersion: "0.17.3", Version: json.RawMessage(`{"id":"26.2"}`)}
	res, err := l.WriteInstance(inst)
	if err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(res.Dir, ATLauncherImageFile)
	if data, err := os.ReadFile(image); err != nil || !bytes.Equal(data, ATLauncherImage) {
		t.Fatalf("new instance should get the shulker image: %v", err)
	}
	if err := os.WriteFile(image, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.WriteInstance(inst); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(image); string(data) != "mine" {
		t.Fatalf("relink replaced the player's image")
	}
}

func readInstanceJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
