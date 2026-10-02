package config

import (
	"strings"
	"testing"
)

// loadNotify loads a config whose top-level notify block is given, as the
// YAML that follows `notify:`.
func loadNotify(t *testing.T, notifyYAML string) (Notify, error) {
	t.Helper()
	cfg, err := Load(writeTempConfig(t, "notify:"+notifyYAML+lockedConfigRest))
	if err != nil {
		return Notify{}, err
	}
	return cfg.Notify, nil
}

func TestNotify_AllThreeKeys(t *testing.T) {
	n, err := loadNotify(t, `
  failure: [alert.sh, page.sh]
  recovery:
    - resolved.sh
  success: [ping.sh]
`)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.Join(n.Failure, ",") != "alert.sh,page.sh" || strings.Join(n.Recovery, ",") != "resolved.sh" || strings.Join(n.Success, ",") != "ping.sh" {
		t.Fatalf("got %+v, want each list under its key", n)
	}
}

func TestNotify_OnlySomeKeys(t *testing.T) {
	n, err := loadNotify(t, `
  failure: [alert.sh]
`)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(n.Failure) != 1 || len(n.Recovery) != 0 || len(n.Success) != 0 {
		t.Fatalf("got %+v, want only a failure command", n)
	}
}

func TestNotify_AbsentOrEmptyBlock(t *testing.T) {
	cfg, err := Load(writeTempConfig(t, lockedConfigRest))
	if err != nil {
		t.Fatalf("Load without a notify block: %v", err)
	}
	if n := cfg.Notify; len(n.Failure)+len(n.Recovery)+len(n.Success) != 0 {
		t.Fatalf("got %+v with no notify block, want nothing", n)
	}
	if _, err := loadNotify(t, "\n"); err != nil {
		t.Fatalf("an empty notify block should be accepted: %v", err)
	}
}

func TestNotify_UnknownKeyRejected(t *testing.T) {
	_, err := loadNotify(t, `
  failure: [alert.sh]
  failed: [alert.sh]
`)
	if err == nil {
		t.Fatal("expected an unknown key to be rejected")
	}
	for _, want := range []string{`unknown key "failed"`, "line 3", "failure, recovery, or success"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("got %q, want it to contain %q", err, want)
		}
	}
}

func TestNotify_WrongShapesRejected(t *testing.T) {
	for name, notifyYAML := range map[string]string{
		"list in place of the map":   " [alert.sh]\n",
		"string in place of the map": " alert.sh\n",
		"string in place of a list":  "\n  failure: alert.sh\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadNotify(t, notifyYAML); err == nil {
				t.Fatal("expected the config to be rejected")
			}
		})
	}
}

func TestNotify_LockedValueRejected(t *testing.T) {
	_, err := loadNotify(t, `
  failure:
    - !locked "`+lockedValue(t)+`"
`)
	if err == nil || !strings.Contains(err.Error(), "notify.failure.0") {
		t.Fatalf("got %v, want a locked value in a notification command rejected", err)
	}
}
