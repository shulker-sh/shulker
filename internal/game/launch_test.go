package game

import (
	"strings"
	"testing"
)

func TestArgvIsTheJVMThenTheMainClassThenTheGame(t *testing.T) {
	v := parse(t, `{
		"id": "26.2", "mainClass": "net.minecraft.client.main.Main",
		"arguments": {"jvm": ["-cp", "${classpath}"], "game": ["--username", "${auth_player_name}", "--uuid", "${auth_uuid}"]}
	}`)
	session := Session{Name: "Notch", UUID: "069a79f4", Token: "mc-token", XUID: "2533", ClientID: "app", Type: "msa"}

	argv := Argv(v, linux64, nil, session.Vars())

	want := []string{"-cp", "${classpath}", "net.minecraft.client.main.Main", "--username", "Notch", "--uuid", "069a79f4"}
	if strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Fatalf("argv %q", argv)
	}
}

func TestSessionFillsEveryPlaceholderTheGameHasForAnAccount(t *testing.T) {
	v := parse(t, `{
		"id": "1.12.2",
		"minecraftArguments": "--username ${auth_player_name} --session ${auth_session} --accessToken ${auth_access_token} --uuid ${auth_uuid} --xuid ${auth_xuid} --clientId ${clientid} --userType ${user_type}"
	}`)
	session := Session{Name: "Notch", UUID: "069a79f4", Token: "mc-token", XUID: "2533", ClientID: "app", Type: "msa"}

	_, game := Args(v, linux64, nil, session.Vars())

	line := strings.Join(game, " ")
	if strings.Contains(line, "${") {
		t.Fatalf("an account leaves no placeholder behind: %q", game)
	}
	// The pre-1.6 pair is the token and the uuid together, which is what a version old enough to
	// ask for it takes instead of the two separate arguments.
	if !strings.Contains(line, "token:mc-token:069a79f4") || !strings.Contains(line, "--userType msa") {
		t.Fatalf("game arguments %q", game)
	}
}
