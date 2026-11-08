package enginebench

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
)

func writeReport(dir string, report Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json.tmp"), append(data, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(dir, "report.json.tmp"), filepath.Join(dir, "report.json")); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "report.html.tmp"))
	if err != nil {
		return err
	}
	err = reportTemplate.Execute(f, report)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(filepath.Join(dir, "report.html.tmp"), filepath.Join(dir, "report.html"))
}

var reportTemplate = template.Must(template.New("report").Funcs(template.FuncMap{
	"milliseconds": func(value *float64) string {
		if value == nil {
			return "—"
		}
		return fmt.Sprintf("%.3f ms", *value)
	},
}).Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>OpenSync engine acceptance benchmark</title>
<style>
body{margin:0;background:#10151e;color:#e9edf3;font:15px/1.6 system-ui,sans-serif}main{max-width:1400px;margin:auto;padding:36px}h1{font-size:32px;margin:0}h2{margin-top:36px}p{max-width:1000px}.cards{display:flex;gap:16px;flex-wrap:wrap}.card{background:#1a2331;padding:16px 24px;border:1px solid #344159;border-radius:8px}.pass{color:#86e3b4}.fail{color:#ffa49d}.blocked{color:#f3cf88}.observation{color:#a8caff}table{border-collapse:collapse;width:100%;font-variant-numeric:tabular-nums}th,td{text-align:left;padding:9px;border-bottom:1px solid #344159;vertical-align:top}th{background:#1a2331}small,.muted{color:#aebbd0}details{padding:8px 0}summary{cursor:pointer}code{overflow-wrap:anywhere}.scroll{overflow-x:auto}a{color:#a8caff}li{margin:5px 0}footer{margin-top:32px;color:#aebbd0}
</style><main>
<h1>OpenSync engine acceptance benchmark</h1>
<p>Known synthetic timing is scored after the engine runs. Real recordings without independent labels are observations. Passing component checks does not certify automatic synchronization.</p>
<div class="cards"><div class="card">Regression gate<br><strong class="{{.RegressionStatus}}">{{.RegressionStatus}}</strong></div><div class="card">Usable-engine readiness<br><strong class="{{.ReadinessStatus}}">{{.ReadinessStatus}}</strong></div><div class="card">Profile<br><strong>{{.Profile}}</strong></div><div class="card">Elapsed<br><strong>{{printf "%.1f" .ElapsedSeconds}} seconds</strong></div></div>
<p class="muted">Run: {{.StartedUTC}} · {{.GoVersion}} · {{.Platform}} · Status: {{.Status}}<br>Primary configuration selected before running: <strong>{{.PrimaryConfiguration}}</strong>. Other configurations are comparisons. Synthetic offset limit: 4 ms for every mode. Runtime has no hardware-independent pass limit.</p>
<h2>Readiness requirements</h2><table><tr><th>Requirement</th><th>Status</th><th>Evidence or missing work</th></tr>{{range .Checks}}<tr><td>{{.Name}}</td><td class="{{.Status}}">{{.Status}}</td><td>{{.Detail}}</td></tr>{{end}}</table>
<h2>Discovery accuracy and alternatives</h2><p>A correct answer at a lower rank does not pass the top-one check. Every declared valid repeated-content alignment must appear. Negative audio is never declared rejected solely because its score is small.</p>
<div class="scroll"><table><tr><th>Dataset / case</th><th>Configuration</th><th>Top error</th><th>Valid alternatives</th><th>Runtime / allocations</th><th>Checks</th></tr>
{{range .Runs}}<tr><td>{{.Dataset}}<br><strong>{{.Scenario}}</strong><br><small>{{.Label}}</small></td><td>{{.Configuration}}</td><td>{{milliseconds .TopErrorMS}}</td><td>{{.AlternativesFound}} / {{.AlternativesExpected}}</td><td>{{printf "%.3f" .ElapsedSeconds}} s<br><small>{{.AllocatedBytes}} bytes allocated</small></td><td>{{range .Checks}}<div class="{{.Status}}">{{.Status}} · {{.Name}}</div><small>{{.Detail}}</small>{{end}}{{if .Error}}<p class="fail">{{.Error}}</p>{{end}}<details><summary>Candidate offsets (seconds)</summary>{{range .Peaks}}<div>{{printf "%+.6f" .OffsetSeconds}} · score {{printf "%.5f" .Score}} · overlap {{printf "%.2f" .OverlapSeconds}} s</div>{{end}}</details></td></tr>
{{end}}</table></div>
<h2>Timing over recording duration</h2><p>Five separate one-second regions. The first two observations fit an affine predictor in the test harness; three later regions check its predictions. Truth is not used to choose validation search centers. This tests the existing local measurement component, not an implemented end-to-end drift correction pipeline.</p>
{{range .Timing}}<h3>{{.Fixture.Name}} · {{.Fixture.DurationSeconds}} seconds</h3><p>Estimated drift {{printf "%.3f" .EstimatedDriftPPM}} ppm; true drift {{printf "%.0f" .Fixture.DriftPPM}} ppm.</p>{{range .Checks}}<div class="{{.Status}}">{{.Status}} · {{.Name}} · {{.Detail}}</div>{{end}}<table><tr><th>Query time</th><th>Role</th><th>Measurement error</th><th>Prediction error</th><th>Error</th></tr>{{range .Points}}<tr><td>{{printf "%.3f" .QuerySeconds}} s</td><td>{{.Role}}</td><td>{{printf "%.3f" .MeasurementErrorMS}} ms</td><td>{{if eq .Role "validation"}}{{printf "%.3f" .PredictionErrorMS}} ms{{else}}not scored{{end}}</td><td>{{.Error}}</td></tr>{{end}}</table>{{end}}
<h2>Scope and limitations</h2><ul>{{range .Limitations}}<li>{{.}}</li>{{end}}</ul>
<details><summary>Build identity and input SHA-256 hashes</summary><pre>{{range $key,$value:=.Build}}{{$key}}: {{$value}}
{{end}}</pre>{{range .Inputs}}<p><code>{{.Path}}</code><br>{{.Bytes}} bytes<br><code>{{.SHA256}}</code></p>{{end}}</details>
<footer>Machine-readable evidence: <a href="report.json">report.json</a>. Generated media stays separate from original recordings. No positions or source media are modified.</footer></main></html>`))
