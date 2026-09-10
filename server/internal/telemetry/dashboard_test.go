package telemetry_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The dashboards and the datasource that serves them are files nothing
// compiles, so what they refer to is checked here instead.
const (
	datasourceFile = "../../../docker/grafana/provisioning/datasources/telemetry.yaml"
	dashboardDir   = "../../../docker/grafana/dashboards"
)

type datasourceFileYAML struct {
	Datasources []struct {
		Name string `yaml:"name"`
		UID  string `yaml:"uid"`
	} `yaml:"datasources"`
}

type dashboard struct {
	UID    string  `json:"uid"`
	Title  string  `json:"title"`
	Panels []panel `json:"panels"`
}

type panel struct {
	Title       string      `json:"title"`
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Datasource  *datasource `json:"datasource"`
	Targets     []struct {
		Datasource *datasource `json:"datasource"`
		Expr       string      `json:"expr"`
	} `json:"targets"`
	Panels []panel `json:"panels"`
}

type datasource struct {
	UID string `json:"uid"`
}

// flatten returns every panel, including those nested inside a row.
func flatten(panels []panel) []panel {
	var out []panel
	for _, p := range panels {
		out = append(out, p)
		out = append(out, flatten(p.Panels)...)
	}
	return out
}

func declaredUIDs(t *testing.T) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(datasourceFile)
	if err != nil {
		t.Fatalf("read %s: %v", datasourceFile, err)
	}
	var f datasourceFileYAML
	if err := yaml.Unmarshal(b, &f); err != nil {
		t.Fatalf("parse %s: %v", datasourceFile, err)
	}
	uids := map[string]bool{}
	for _, d := range f.Datasources {
		if d.UID == "" {
			t.Errorf("datasource %q declares no uid; a generated one orphans every panel that names it", d.Name)
		}
		uids[d.UID] = true
	}
	if len(uids) == 0 {
		t.Fatalf("%s declares no datasources", datasourceFile)
	}
	return uids
}

func dashboards(t *testing.T) map[string]dashboard {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dashboardDir, "*.json"))
	if err != nil {
		t.Fatalf("glob %s: %v", dashboardDir, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no dashboards under %s", dashboardDir)
	}
	out := map[string]dashboard{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		var d dashboard
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		out[filepath.Base(p)] = d
	}
	return out
}

// TestDashboardsNameADeclaredDatasource is the check the fixed uid exists for:
// a panel naming a uid nothing provisions renders as an error rather than as
// no data.
func TestDashboardsNameADeclaredDatasource(t *testing.T) {
	uids := declaredUIDs(t)
	for name, d := range dashboards(t) {
		if d.UID == "" {
			t.Errorf("%s declares no uid of its own", name)
		}
		for _, p := range flatten(d.Panels) {
			if p.Datasource != nil && !uids[p.Datasource.UID] {
				t.Errorf("%s panel %q names datasource uid %q, which no datasource declares", name, p.Title, p.Datasource.UID)
			}
			for _, tg := range p.Targets {
				if tg.Datasource != nil && !uids[tg.Datasource.UID] {
					t.Errorf("%s panel %q has a target naming datasource uid %q, which no datasource declares", name, p.Title, tg.Datasource.UID)
				}
			}
		}
	}
}

// TestPanelsAreDescribed keeps a panel from shipping without saying what it
// means, which is what stops it being read as something it is not.
func TestPanelsAreDescribed(t *testing.T) {
	for name, d := range dashboards(t) {
		for _, p := range flatten(d.Panels) {
			if p.Type == "row" {
				continue
			}
			if strings.TrimSpace(p.Description) == "" {
				t.Errorf("%s panel %q carries no description", name, p.Title)
			}
		}
	}
}

// TestPanelsQueryProducedMetrics fails a panel reading a stonks metric nothing
// emits, so a rename shows up here rather than as an empty panel weeks later.
func TestPanelsQueryProducedMetrics(t *testing.T) {
	for name, d := range dashboards(t) {
		for _, p := range flatten(d.Panels) {
			for _, tg := range p.Targets {
				for _, metric := range stonksMetrics(tg.Expr) {
					if !producedMetrics[metric] {
						t.Errorf("%s panel %q reads %q, which nothing emits; the names the service produces are %v",
							name, p.Title, metric, keys(producedMetrics))
					}
				}
			}
		}
	}
}

// producedMetrics are the Prometheus names of the stonks_ series the service
// emits, which is each instrument declared in
// [metrics.go](../auth/metrics.go) with the suffix the exporter adds to a
// monotonic sum. A hand-written instrument is added here as it is added to the
// code.
var producedMetrics = map[string]bool{
	"stonks_auth_sign_ins_total":          true,
	"stonks_auth_users_provisioned_total": true,
	"stonks_session_creations_total":      true,
	"stonks_session_deletions_total":      true,
	"stonks_session_lookups_total":        true,
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// isIdent reports whether r can appear in a metric name.
func isIdent(r rune) bool {
	switch {
	case r == '_':
		return true
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return false
}

// stonksMetrics returns the stonks_ prefixed metric names an expression reads.
func stonksMetrics(expr string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(expr, func(r rune) bool { return !isIdent(r) }) {
		if strings.HasPrefix(f, "stonks_") {
			out = append(out, f)
		}
	}
	return out
}
