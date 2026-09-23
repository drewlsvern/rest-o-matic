package config

import (
	"reflect"
	"strings"
	"testing"
)

func loadAfter(t *testing.T, hooksYAML string) (AfterHooks, error) {
	t.Helper()
	path := writeTempConfig(t, `
backups:
  j:
    source: {paths: ["/a"]}
    hooks:
`+hooksYAML)
	cfg, err := Load(path)
	if err != nil {
		return AfterHooks{}, err
	}
	return cfg.Backups["j"].Hooks.After, nil
}

func TestAfterHooks_Accepted(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want AfterHooks
	}{
		{
			name: "block list is always",
			yaml: "      after:\n        - start.sh\n        - ping.sh\n",
			want: AfterHooks{Always: []string{"start.sh", "ping.sh"}},
		},
		{
			name: "inline list is always",
			yaml: "      after: [start.sh]\n",
			want: AfterHooks{Always: []string{"start.sh"}},
		},
		{
			name: "full map",
			yaml: "      after:\n        always: [start.sh]\n        success: [ping.sh]\n        failure: [alert.sh]\n",
			want: AfterHooks{Always: []string{"start.sh"}, Success: []string{"ping.sh"}, Failure: []string{"alert.sh"}},
		},
		{
			name: "partial map",
			yaml: "      after:\n        failure: [alert.sh]\n",
			want: AfterHooks{Failure: []string{"alert.sh"}},
		},
		{
			name: "inline map",
			yaml: "      after: {always: [start.sh], failure: [alert.sh]}\n",
			want: AfterHooks{Always: []string{"start.sh"}, Failure: []string{"alert.sh"}},
		},
		{
			name: "empty after",
			yaml: "      after:\n",
			want: AfterHooks{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadAfter(t, tt.yaml)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAfterHooks_UnknownKeyRejected(t *testing.T) {
	_, err := loadAfter(t, "      after:\n        always: [start.sh]\n        sucess: [ping.sh]\n")
	if err == nil {
		t.Fatal("expected an error for an unknown key")
	}
	for _, want := range []string{`"sucess"`, "line 8", "always, success, or failure"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected error to mention %q, got: %v", want, err)
		}
	}
}

func TestAfterHooks_BareStringRejected(t *testing.T) {
	if _, err := loadAfter(t, "      after: start.sh\n"); err == nil {
		t.Fatal("expected an error for a bare string")
	}
}
