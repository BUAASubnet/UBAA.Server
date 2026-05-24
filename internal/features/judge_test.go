package features

import (
	"testing"

	"github.com/BUAASubnet/UBAA.Server/internal/dto"
)

func TestJudgeParseBriefAnswerRowsWithTHNumberColumn(t *testing.T) {
	html := `
		<html><body>
		  作业时间：2026-04-20 19:00:00 至 2026-05-03 23:00:00
		  作业满分： 100.00 ，共 2道 题
		  <h5>简答题</h5>
		  <table>
		    <thead><tr><th>#</th><th>题目</th><th>分值</th><th>提交/评阅状态</th></tr></thead>
		    <tbody>
		      <tr><th>1.</th><td><a>设计说明</a></td><td>60.00</td><td>初次提交时间: 2026-04-17 12:24:26 最后一次修改时间: 2026-04-17 12:24:26</td></tr>
		      <tr><th>2.</th><td><a>用例设计</a></td><td>40.00</td><td>未提交答案</td></tr>
		    </tbody>
		  </table>
		</body></html>
	`

	detail := parseJudgeAssignmentDetail(html, "1", "软件工程", "101", "sample")

	if detail.StartTime == nil || *detail.StartTime != "2026-04-20 19:00:00" {
		t.Fatalf("startTime = %#v", detail.StartTime)
	}
	if detail.DueTime == nil || *detail.DueTime != "2026-05-03 23:00:00" {
		t.Fatalf("dueTime = %#v", detail.DueTime)
	}
	if detail.MaxScore == nil || *detail.MaxScore != "100" {
		t.Fatalf("maxScore = %#v", detail.MaxScore)
	}
	if detail.MyScore != nil {
		t.Fatalf("myScore = %#v", detail.MyScore)
	}
	if detail.TotalProblems != 2 {
		t.Fatalf("totalProblems = %d", detail.TotalProblems)
	}
	if detail.SubmittedCount != 1 {
		t.Fatalf("submittedCount = %d", detail.SubmittedCount)
	}
	if detail.SubmissionStatus != "PARTIAL" {
		t.Fatalf("submissionStatus = %q", detail.SubmissionStatus)
	}
	assertJudgeProblemNames(t, detail.Problems, []string{"设计说明", "用例设计"})
	assertJudgeProblemStatuses(t, detail.Problems, []string{"SUBMITTED", "UNSUBMITTED"})
}

func TestJudgeParseProgrammingRowsWithoutCountingNestedTestcaseTables(t *testing.T) {
	html := `
		<html><body>
		  作业满分： 20.00 ，共 2道 题
		  <h5>编程题</h5>
		  <table>
		    <thead><tr><th>#</th><th>题目</th><th>分值</th><th>批阅信息</th></tr></thead>
		    <tbody>
		      <tr>
		        <th>1.</th><td>程序一</td><td>10.00</td>
		        <td>下载源文件 最后一次提交时间：2026-04-17 12:00:00 得分：8.00
		          <table><tr><th>name</th><th>verdict</th></tr><tr><td>TestCase1</td><td>Accept</td></tr></table>
		        </td>
		      </tr>
		      <tr><th>2.</th><td>程序二</td><td>10.00</td><td>还未提交代码 详细</td></tr>
		    </tbody>
		  </table>
		</body></html>
	`

	detail := parseJudgeAssignmentDetail(html, "1", "算法", "102", "sample")

	if detail.TotalProblems != 2 {
		t.Fatalf("totalProblems = %d", detail.TotalProblems)
	}
	if detail.SubmittedCount != 1 {
		t.Fatalf("submittedCount = %d", detail.SubmittedCount)
	}
	if detail.MyScore == nil || *detail.MyScore != "8" {
		t.Fatalf("myScore = %#v", detail.MyScore)
	}
	assertJudgeProblemNames(t, detail.Problems, []string{"程序一", "程序二"})
}

func TestJudgeParseFileUploadRows(t *testing.T) {
	html := `
		<html><body>
		  作业满分： 10.00 ，共 2道 题
		  <h5>文件上传题</h5>
		  <table>
		    <thead><tr><th>#</th><th>题目</th><th>分值</th><th>提交状态</th></tr></thead>
		    <tbody>
		      <tr><th>1.</th><td>任务一</td><td>5.00</td><td>未提交文件</td></tr>
		      <tr><th>2.</th><td>任务二</td><td>5.00</td><td>初次提交时间: 2026-04-09 15:03:32 最近一次提交时间: 2026-04-09 15:03:30 文件重命名为: 24182104.pdf 下载</td></tr>
		    </tbody>
		  </table>
		</body></html>
	`

	detail := parseJudgeAssignmentDetail(html, "1", "工程实践", "103", "sample")

	if detail.SubmittedCount != 1 {
		t.Fatalf("submittedCount = %d", detail.SubmittedCount)
	}
	assertJudgeProblemStatuses(t, detail.Problems, []string{"UNSUBMITTED", "SUBMITTED"})
}

func TestJudgeParseChoiceAndFillRowsWithTwoCells(t *testing.T) {
	html := `
		<html><body>
		  作业满分： 2.00 ，共 2道 题
		  <h5>选择题</h5>
		  <table>
		    <tbody>
		      <tr><th>1.</th><td>已提交 首次提交时间: 2026-04-14 19:38:38 最后一次提交时间: 2026-04-14 19:38:39 题干 得分：1.00</td></tr>
		      <tr><th>2.</th><td>未作答 题干</td></tr>
		    </tbody>
		  </table>
		</body></html>
	`

	detail := parseJudgeAssignmentDetail(html, "1", "概率统计", "104", "sample")

	if detail.TotalProblems != 2 {
		t.Fatalf("totalProblems = %d", detail.TotalProblems)
	}
	if detail.SubmittedCount != 1 {
		t.Fatalf("submittedCount = %d", detail.SubmittedCount)
	}
	if detail.MyScore == nil || *detail.MyScore != "1" {
		t.Fatalf("myScore = %#v", detail.MyScore)
	}
	assertJudgeProblemNames(t, detail.Problems, []string{"第1题", "第2题"})
}

func assertJudgeProblemNames(t *testing.T, problems []dto.JudgeProblemDto, want []string) {
	t.Helper()
	if len(problems) != len(want) {
		t.Fatalf("problem count = %d, want %d", len(problems), len(want))
	}
	for i := range want {
		if problems[i].Name != want[i] {
			t.Fatalf("problem[%d].Name = %q, want %q", i, problems[i].Name, want[i])
		}
	}
}

func assertJudgeProblemStatuses(t *testing.T, problems []dto.JudgeProblemDto, want []string) {
	t.Helper()
	if len(problems) != len(want) {
		t.Fatalf("problem count = %d, want %d", len(problems), len(want))
	}
	for i := range want {
		if problems[i].Status != want[i] {
			t.Fatalf("problem[%d].Status = %q, want %q", i, problems[i].Status, want[i])
		}
	}
}
