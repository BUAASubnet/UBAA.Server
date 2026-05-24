package features

import (
	"encoding/json"
	"testing"

	"github.com/BUAASubnet/UBAA.Server/internal/dto"
)

func TestEvaluationMergeCoursesMatchesKtorBehavior(t *testing.T) {
	courseMap := map[string]dto.EvaluationCourse{}
	order := []string{}
	bpdm1 := "T001"
	bpmc1 := "李老师"
	pjrdm := "22373333"
	pjrmc := "测试学生"
	zdmc := "STID"
	ypjcs := 0
	xypjcs := 1
	sxz := "1"
	rwh := "rwh-1"
	xn := "2025-2026"
	xq := "1"
	pjlxid := "2"
	sfksqbpj := "1"
	yxsfktjst := "0"
	mergeEvaluationCourses(courseMap, &order, []evaluationCourseRaw{
		{
			Kcdm:      "CS101",
			Kcmc:      "操作系统",
			Bpmc:      &bpmc1,
			Bpdm:      &bpdm1,
			Pjrdm:     &pjrdm,
			Pjrmc:     &pjrmc,
			Zdmc:      &zdmc,
			Ypjcs:     &ypjcs,
			Xypjcs:    &xypjcs,
			Sxz:       &sxz,
			Rwh:       &rwh,
			Xn:        &xn,
			Xq:        &xq,
			Pjlxid:    &pjlxid,
			Sfksqbpj:  &sfksqbpj,
			Yxsfktjst: &yxsfktjst,
		},
	}, "rw1", "wj1", "2025-20261", "2", false)
	bpdm2 := "T002"
	bpmc2 := "王老师"
	mergeEvaluationCourses(courseMap, &order, []evaluationCourseRaw{
		{Kcdm: "CS102", Kcmc: "编译原理", Bpmc: &bpmc2, Bpdm: &bpdm2, Pjrdm: &pjrdm, Pjrmc: &pjrmc},
	}, "rw1", "wj1", "2025-20261", "2", true)

	if len(order) != 2 {
		t.Fatalf("order = %#v", order)
	}
	pending := courseMap["rw1_wj1_CS101_T001"]
	if pending.ID != "rw1_wj1_CS101_T001" || pending.IsEvaluated {
		t.Fatalf("pending course = %#v", pending)
	}
	if pending.Xnxq == nil || *pending.Xnxq != "2025-20261" {
		t.Fatalf("pending.Xnxq = %#v", pending.Xnxq)
	}
	if pending.Msid != "2" {
		t.Fatalf("pending.Msid = %q", pending.Msid)
	}
	evaluated := courseMap["rw1_wj1_CS102_T002"]
	if evaluated.ID != "rw1_wj1_CS102_T002" || !evaluated.IsEvaluated {
		t.Fatalf("evaluated course = %#v", evaluated)
	}
}

func TestEvaluationBuildSubmitPayloadMatchesKtorShape(t *testing.T) {
	var topic map[string]any
	raw := `{
		"pjxtWjWjbReturnEntity": {
			"wjzblist": [
				{"tklist": [
					{"tmlx": "1", "tmid": "q1", "tmxxlist": [{"tmxxid": "optA"}, {"tmxxid": "optB"}]}
				]}
			]
		},
		"pjxtPjjgPjjgckb": [
			{
				"wjssrwid": "ssrw1",
				"bprdm": "T001",
				"bprmc": "李老师",
				"kcdm": "CS101",
				"kcmc": "操作系统",
				"pjfs": "1",
				"pjid": "pj1",
				"pjlx": "2",
				"pjrdm": "22373333",
				"pjrjsdm": "22373333",
				"pjrxm": "测试学生",
				"xnxq": "2025-20261",
				"sfxxpj": "1"
			}
		],
		"pjmap": {"source": "test"}
	}`
	if err := json.Unmarshal([]byte(raw), &topic); err != nil {
		t.Fatal(err)
	}
	payload, ok := buildEvaluationResultsPayload(dto.EvaluationCourse{
		Kcmc: "操作系统",
		Rwid: "rw1",
		Wjid: "wj1",
		Kcdm: "CS101",
		Bpmc: "李老师",
	}, topic)
	if !ok || len(payload) != 1 {
		t.Fatalf("payload ok=%v len=%d", ok, len(payload))
	}
	item := payload[0]
	if item["pjdf"] != 93 || item["stzjid"] != "xx" || item["sfnm"] != "1" {
		t.Fatalf("payload fixed fields = %#v", item)
	}
	questions, ok := item["pjxxlist"].([]map[string]any)
	if !ok || len(questions) != 1 {
		t.Fatalf("pjxxlist = %#v", item["pjxxlist"])
	}
	if questions[0]["wjid"] != "wj1" || questions[0]["wjstid"] != "q1" {
		t.Fatalf("question = %#v", questions[0])
	}
	answers, ok := questions[0]["xxdalist"].([]any)
	if !ok || len(answers) != 1 {
		t.Fatalf("answers = %#v", questions[0]["xxdalist"])
	}
}

func TestEvaluationProgressComputedFields(t *testing.T) {
	progress := evaluationProgress(2, 1, 1)
	if progress.ProgressPercent != 50 || progress.IsCompleted {
		t.Fatalf("progress = %#v", progress)
	}
	progress = evaluationProgress(2, 2, 0)
	if progress.ProgressPercent != 100 || !progress.IsCompleted {
		t.Fatalf("completed progress = %#v", progress)
	}
}
