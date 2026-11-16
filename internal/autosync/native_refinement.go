package autosync

// Discovery, model selection, and audit use separate temporal regions. These
// gates deliberately retain the established verifier's thresholds.
import (
	"math"
	"sort"
)

type NativePrior struct {
	Offset   float64 `json:"offset"`
	Slope    float64 `json:"slope"`
	Strength float64 `json:"prior_strength"`
	Method   string  `json:"discovery_method,omitempty"`
	Score    float64 `json:"discovery_score,omitempty"`
}
type ModelJudgment struct {
	Supported int      `json:"supported_windows"`
	Ambiguous int      `json:"ambiguous_windows"`
	Maximum   *float64 `json:"maximum_residual_seconds"`
	Median    *float64 `json:"median_residual_seconds"`
	Passes    bool     `json:"passes"`
}
type NativeRelationModel struct {
	Kind      string        `json:"kind"`
	Offset    float64       `json:"offset,omitempty"`
	Slope     float64       `json:"slope,omitempty"`
	Knots     []TimePoint   `json:"knots,omitempty"`
	Selection ModelJudgment `json:"selection"`
}
type NativeProfile struct {
	VerificationMode string                 `json:"verification_mode,omitempty"`
	ShortWindows     []*ShortWindowEvidence `json:"short_window_evidence,omitempty"`
	Prior            NativePrior            `json:"prior"`
	Status           string                 `json:"status"`
	Overlap          float64                `json:"overlap_seconds"`
	Training         []*Measurement         `json:"training,omitempty"`
	Selection        []*Measurement         `json:"selection,omitempty"`
	Audit            []*Measurement         `json:"audit,omitempty"`
	Models           []*NativeRelationModel `json:"models,omitempty"`
	Model            *NativeRelationModel   `json:"model,omitempty"`
	AuditResult      *ModelJudgment         `json:"audit_result,omitempty"`
	Confidence       float64                `json:"confidence,omitempty"`
	Limit            float64                `json:"timing_limit_seconds,omitempty"`
	Range            []float64              `json:"validated_source_range,omitempty"`
	Supported        int                    `json:"supported_windows,omitempty"`
}
type NativeProfilePair struct {
	Reference int              `json:"reference"`
	Query     int              `json:"query"`
	Profiles  []*NativeProfile `json:"profiles"`
	Digest    string           `json:"input_digest"`
}

func (m *NativeRelationModel) predict(t float64) float64 {
	if m.Kind != "piecewise_linear" {
		return m.Offset + m.Slope*t
	}
	if t <= m.Knots[0][0] {
		return m.Knots[0][1]
	}
	if t >= m.Knots[len(m.Knots)-1][0] {
		return m.Knots[len(m.Knots)-1][1]
	}
	i := sort.Search(len(m.Knots), func(i int) bool { return m.Knots[i][0] >= t })
	a, b := m.Knots[i-1], m.Knots[i]
	return a[1] + (b[1]-a[1])*(t-a[0])/(b[0]-a[0])
}
func fitNativeModels(observations []*Measurement) []*NativeRelationModel {
	good := append([]*Measurement(nil), observations...)
	sort.SliceStable(good, func(i, j int) bool { return good[i].QueryTime < good[j].QueryTime })
	if len(good) < 2 {
		return nil
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, o := range good {
		lo = math.Min(lo, o.Offset)
		hi = math.Max(hi, o.Offset)
	}
	models := []*NativeRelationModel{{Kind: "constant", Offset: (lo + hi) / 2}}
	span := good[len(good)-1].QueryTime - good[0].QueryTime
	if span >= 20 && len(good) >= 3 {
		var slopes []float64
		for i, a := range good {
			for _, b := range good[i+1:] {
				if b.QueryTime > a.QueryTime {
					slopes = append(slopes, (b.Offset-a.Offset)/(b.QueryTime-a.QueryTime))
				}
			}
		}
		slope := median(slopes)
		offsets := make([]float64, len(good))
		for i, o := range good {
			offsets[i] = o.Offset - slope*o.QueryTime
		}
		if math.Abs(slope) <= .002 {
			models = append(models, &NativeRelationModel{Kind: "affine", Offset: median(offsets), Slope: slope})
		}
	}
	if len(good) >= 5 && span >= 30 {
		valid := true
		points := make([]TimePoint, len(good))
		for i, o := range good {
			points[i] = TimePoint{o.QueryTime, o.Offset}
			if i > 0 {
				p := good[i-1]
				dt := o.QueryTime - p.QueryTime
				if dt <= 0 || dt+o.Offset-p.Offset <= 0 || math.Abs((o.Offset-p.Offset)/dt) > .02 {
					valid = false
				}
			}
		}
		if valid {
			models = append(models, &NativeRelationModel{Kind: "piecewise_linear", Knots: points})
		}
	}
	return models
}
func judgeNative(m *NativeRelationModel, observations []*Measurement, limit float64, minimum int) ModelJudgment {
	result := ModelJudgment{}
	var residuals []float64
	for _, o := range observations {
		if !o.Supported {
			continue
		}
		r := math.Abs(o.Offset - m.predict(o.QueryTime))
		if o.Precise || r <= limit {
			residuals = append(residuals, r)
		} else {
			result.Ambiguous++
		}
	}
	result.Supported = len(residuals)
	if len(residuals) > 0 {
		maximum := 0.
		for _, r := range residuals {
			maximum = math.Max(maximum, r)
		}
		med := median(residuals)
		result.Maximum = &maximum
		result.Median = &med
		result.Passes = len(residuals) >= minimum && maximum <= limit
	}
	return result
}
func (a *NativeAnalyzer) measure(ref, query int, center, length float64, prior NativePrior) (*Measurement, error) {
	offset := prior.Offset + prior.Slope*center
	start := center - length/2
	prepared, err := a.prepareQuery(query, start, length)
	if err != nil {
		return nil, err
	}
	var best *Measurement
	for _, radius := range []float64{.35, 1.5} {
		m, err := a.waveformPrepared(ref, prepared, offset, radius)
		if err != nil {
			return nil, err
		}
		if m == nil {
			continue
		}
		count, strongest, err := a.controlsPrepared(ref, prepared, offset, radius, []float64{-31, -15, 15, 31})
		if err != nil {
			return nil, err
		}
		if count < 2 {
			var shifts []float64
			for _, shift := range []float64{-8, -4, -2, 2, 4, 8} {
				if math.Abs(shift) > length+2*radius {
					shifts = append(shifts, shift)
				}
			}
			n, score, err := a.controlsPrepared(ref, prepared, offset, radius, shifts)
			if err != nil {
				return nil, err
			}
			if n > 0 {
				if count == 0 {
					strongest = score
				} else {
					strongest = math.Max(strongest, score)
				}
				count += n
			}
		}
		m.Flux, err = a.fluxPrepared(ref, prepared, m.Offset)
		if err != nil {
			return nil, err
		}
		central, err := a.waveform(ref, query, center-length/4, m.Offset, length/2, .08)
		if err != nil {
			return nil, err
		}
		m.Phase, err = a.phasePrepared(ref, prepared, offset, radius)
		if err != nil {
			return nil, err
		}
		m.PhaseAgrees = m.Phase != nil && m.Phase.Distinct && math.Abs(m.Phase.Offset-m.Offset) <= .008
		m.Stable = central != nil && math.Abs(central.Offset-m.Offset) <= .008
		if central != nil {
			v := central.Offset
			m.Central = &v
		}
		m.Boundary = math.Abs(m.Offset-offset) >= radius-.002 && start+offset-radius > 0 && start+offset+length+radius < a.records[ref].Duration
		m.Controls = count
		m.Control = strongest
		m.Radius = radius
		m.Supported = !m.Boundary && signalSupported(m, count, strongest, m.Flux)
		m.Precise = m.Supported && (m.Distinct || m.PhaseAgrees) && (m.Stable || m.PhaseAgrees)
		if best == nil || (m.Supported && !best.Supported) || (m.Supported == best.Supported && m.Wave-m.Control > best.Wave-best.Control) {
			best = m
		}
		if m.Supported {
			break
		}
	}
	if best != nil && !best.Supported && length > 1 && best.PhaseAgrees && best.Flux > .5 {
		shorter, err := a.measure(ref, query, center, length/2, prior)
		if err != nil {
			return nil, err
		}
		if shorter != nil && shorter.Supported {
			return shorter, nil
		}
	}
	return best, nil
}
func (a *NativeAnalyzer) profileCandidate(ref, query int, prior NativePrior) (*NativeProfile, error) {
	profile, err := a.profileCandidateWindows(ref, query, prior, false)
	if err != nil || !longSpectralWindows(prior, profile.Overlap) || profile.Model != nil {
		return profile, err
	}
	longer, err := a.profileCandidateWindows(ref, query, prior, true)
	if err != nil || longer.Model != nil {
		return longer, err
	}
	return profile, nil
}

func longSpectralWindows(prior NativePrior, overlap float64) bool {
	return prior.Method == "distributed_spectral" && prior.Score >= .18 && overlap >= 15 && overlap < 30
}

func (a *NativeAnalyzer) profileCandidateWindows(ref, query int, prior NativePrior, longer bool) (*NativeProfile, error) {
	rate := 1 + prior.Slope
	begin := math.Max(0, -prior.Offset/rate) + .15
	end := math.Min(a.records[query].Duration, (a.records[ref].Duration-prior.Offset)/rate) - .15
	overlap := end - begin
	result := &NativeProfile{Prior: prior, Status: "insufficient_evidence", Overlap: overlap}
	if overlap < 1.8 {
		return result, nil
	}
	count := min(129, max(3, int(overlap/10)+1))
	length := math.Min(6, overlap/float64(5*(count-1)))
	if length < .2 {
		return result, nil
	}
	var schedules [3][]TimePoint
	centers := linspace(begin+length/2, end-length/2, count)
	for i, c := range centers {
		schedules[0] = append(schedules[0], TimePoint{c, length})
		if i+1 < len(centers) {
			next := centers[i+1]
			schedules[1] = append(schedules[1], TimePoint{(c + next) / 2, length / 2})
			for _, fraction := range []float64{1. / 3, 2. / 3} {
				schedules[2] = append(schedules[2], TimePoint{c + (next-c)*fraction, length / 2})
			}
		}
	}
	if overlap < 30 {
		schedules = [3][]TimePoint{}
		block := overlap / 12
		for role := range schedules {
			for j := role; j < 12; j += 3 {
				schedules[role] = append(schedules[role], TimePoint{begin + (float64(j)+.5)*block, .9 * block})
			}
		}
	}
	// Strong spectral candidates can need more waveform context in a
	// noisy room. Use guarded, longer windows while retaining held-out roles.
	if longer {
		schedules = [3][]TimePoint{}
		block := overlap / 7
		for j, role := range []int{0, 1, 0, 2, 0, 1, 2} {
			schedules[role] = append(schedules[role], TimePoint{begin + (float64(j)+.5)*block, .95 * block})
		}
	}
	outputs := []*[]*Measurement{&result.Training, &result.Selection, &result.Audit}
	type measurementJob struct {
		role   int
		window TimePoint
	}
	jobs := []measurementJob{}
	for role, schedule := range schedules {
		for _, s := range schedule {
			jobs = append(jobs, measurementJob{role, s})
		}
	}
	measurements := make([]*Measurement, len(jobs))
	errs := make([]error, len(jobs))
	a.computeTasks(len(jobs), func(i int) {
		if err := a.ctx.Err(); err != nil {
			errs[i] = err
			return
		}
		s := jobs[i].window
		measurements[i], errs[i] = a.measure(ref, query, s[0], s[1], prior)
	})
	for i, m := range measurements {
		if errs[i] != nil {
			return nil, errs[i]
		}
		if m != nil {
			*outputs[jobs[i].role] = append(*outputs[jobs[i].role], m)
		}
	}
	evaluateNativeProfile(result)
	return result, nil
}
func evaluateNativeProfile(result *NativeProfile) {
	result.Status = "insufficient_evidence"
	result.Model = nil
	result.Models = nil
	result.AuditResult = nil
	result.Range = nil
	result.Confidence = 0
	result.Limit = 0
	result.Supported = 0
	var training []*Measurement
	for _, o := range result.Training {
		if o.Precise {
			training = append(training, o)
		}
	}
	required := 2
	if result.Overlap >= 30 {
		required = 3
	}
	if len(training) < required {
		return
	}
	models := fitNativeModels(training)
	var selected *NativeRelationModel
	limit := .025
	for _, bound := range []float64{.025, .05} {
		limit = bound
		for _, m := range models {
			minimum := 2
			if result.Overlap < 30 {
				minimum = 1
			}
			m.Selection = judgeNative(m, result.Selection, limit, minimum)
			if m.Selection.Passes && judgeNative(m, training, limit, 2).Passes {
				selected = m
				break
			}
		}
		if selected != nil {
			break
		}
	}
	result.Models = models
	result.Status = "event_supported_timing_unresolved"
	if selected == nil {
		return
	}
	checked := judgeNative(selected, result.Audit, limit, 2)
	result.AuditResult = &checked
	if !checked.Passes {
		checked = judgeNative(selected, result.Audit, .05, 2)
		if !checked.Passes {
			return
		}
		limit = .05
		result.AuditResult = &checked
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	var strengths []float64
	for _, role := range [][]*Measurement{result.Training, result.Selection, result.Audit} {
		for _, o := range role {
			if o.Supported && math.Abs(o.Offset-selected.predict(o.QueryTime)) <= limit {
				lo = math.Min(lo, o.QueryTime-o.Window/2)
				hi = math.Max(hi, o.QueryTime+o.Window/2)
				strengths = append(strengths, o.Wave-o.Control)
			}
		}
	}
	if len(strengths) == 0 || (result.Overlap >= 30 && hi-lo < .8*result.Overlap) {
		return
	}
	result.Status = "validated_relation"
	if limit > .025 || selected.Selection.Supported < 2 || (result.Overlap >= 12 && len(training) < 3) {
		result.Status = "corroboration_required"
	}
	result.Model = selected
	result.Confidence = median(strengths)
	result.Limit = limit
	result.Range = []float64{lo, hi}
	result.Supported = len(strengths)
}
