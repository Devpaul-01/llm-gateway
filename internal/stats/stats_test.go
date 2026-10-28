package stats

import "testing"

func TestSplitProviders(t *testing.T) {
	got := SplitProviders(map[string]int64{
		"up_ok:groq":                   10,
		"up_fail:groq:key_fault":       3,
		"up_fail:groq:transient":       1,
		"up_fail:openrouter:bad_model": 2,
		"rate_limited":                 99,
	})
	g := got["groq"]
	if g == nil || g.OK != 10 || g.Failed != 4 || g.Failures["key_fault"] != 3 {
		t.Errorf("groq: %+v", g)
	}
	if o := got["openrouter"]; o == nil || o.OK != 0 || o.Failed != 2 {
		t.Errorf("openrouter: %+v", o)
	}
	if len(got) != 2 {
		t.Errorf("non-provider counters must be ignored: %+v", got)
	}
}
