package launcher

import (
	"testing"

	"shulker.sh/shulker/internal/account"
)

func TestReadMultiMCAccounts(t *testing.T) {
	dir := writePrism(t, `{
	  "formatVersion": 3,
	  "accounts": [
	    {
	      "type": "MSA",
	      "msa": {"iat": 1789862400, "exp": 1789948800, "token": "m", "refresh_token": "r"},
	      "utoken": {"token": "u", "extra": {"uhs": "h"}},
	      "xrp-main": {"token": "x"},
	      "xrp-mc": {"token": "y"},
	      "ygg": {"iat": 1789862400, "exp": 1789948800, "token": "session"},
	      "profile": {"id": "069a79f44e9a4726a5befca90e38aaf5", "name": "Notch", "skin": {"id": "", "url": "", "variant": ""}, "capes": []},
	      "entitlement": {"ownsMinecraft": true, "canPlayMinecraft": true},
	      "active": true
	    },
	    {
	      "type": "MSA",
	      "msa": {"token": "m", "refresh_token": "r"},
	      "ygg": {"token": "t"},
	      "entitlement": {"ownsMinecraft": false, "canPlayMinecraft": false}
	    }
	  ]
	}`)

	got, errs := multimcEntry.Accounts(multimcEntry, dir, prismNow)
	if errs != nil {
		t.Fatal(errs)
	}
	if len(got) != 1 {
		t.Fatalf("an account with no Java profile isn't listed: %+v", got)
	}
	notch := got[0]
	if notch.ID != "069a79f44e9a4726a5befca90e38aaf5" || notch.Name != "Notch" || notch.Source != "multimc" || notch.Group != account.GroupLauncher {
		t.Errorf("row = %+v", notch)
	}
	if notch.State != account.Playable || notch.Account.Minecraft == nil || notch.Account.Minecraft.Token != "session" {
		t.Errorf("a token that still holds is playable: %+v", notch)
	}
}

func TestReadMultiMCOtherVersionWarns(t *testing.T) {
	dir := writePrism(t, `{"formatVersion": 2, "accounts": []}`)
	if _, errs := multimcEntry.Accounts(multimcEntry, dir, prismNow); len(errs) != 1 {
		t.Fatalf("a list MultiMC itself would rename away is warned about: %v", errs)
	}
}
