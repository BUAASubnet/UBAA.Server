package features

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"github.com/BUAASubnet/UBAA.Server/internal/httpx"
	"github.com/gofiber/fiber/v2"
)

func (s *Service) GetEvaluationCourses(ctx context.Context, username string) (dto.EvaluationCoursesResponse, error) {
	client := &evaluationClient{username: username, service: s}
	if ok, err := client.InitSession(ctx); err != nil {
		return dto.EvaluationCoursesResponse{}, err
	} else if !ok {
		return emptyEvaluationCoursesResponse(), nil
	}
	xnxq, err := client.CurrentXnxq(ctx)
	if err != nil || xnxq == "" {
		return emptyEvaluationCoursesResponse(), nil
	}
	tasks, err := client.Tasks(ctx)
	if err != nil {
		return emptyEvaluationCoursesResponse(), nil
	}
	courseMap := map[string]dto.EvaluationCourse{}
	order := []string{}
	for _, task := range tasks {
		questionnaires, _ := client.Questionnaires(ctx, task.Rwid)
		for _, questionnaire := range questionnaires {
			msid := strings.TrimSpace(questionnaire.Msid)
			if msid == "" {
				msid = "1"
			}
			pending, _ := client.Courses(ctx, task.Rwid, questionnaire.Wjid, xnxq, msid, "0")
			mergeEvaluationCourses(courseMap, &order, pending, task.Rwid, questionnaire.Wjid, xnxq, msid, false)
			evaluated, _ := client.Courses(ctx, task.Rwid, questionnaire.Wjid, xnxq, msid, "1")
			mergeEvaluationCourses(courseMap, &order, evaluated, task.Rwid, questionnaire.Wjid, xnxq, msid, true)
		}
	}
	courses := make([]dto.EvaluationCourse, 0, len(courseMap))
	for _, key := range order {
		if course, ok := courseMap[key]; ok {
			courses = append(courses, course)
		}
	}
	sort.SliceStable(courses, func(i, j int) bool {
		return !courses[i].IsEvaluated && courses[j].IsEvaluated
	})
	pendingCount := 0
	evaluatedCount := 0
	for _, course := range courses {
		if course.IsEvaluated {
			evaluatedCount++
		} else {
			pendingCount++
		}
	}
	return dto.EvaluationCoursesResponse{
		Courses:  courses,
		Progress: evaluationProgress(len(courses), evaluatedCount, pendingCount),
	}, nil
}

func (s *Service) AutoEvaluate(ctx context.Context, username string, courses []dto.EvaluationCourse) ([]dto.EvaluationResult, error) {
	client := &evaluationClient{username: username, service: s}
	if _, err := client.InitSession(ctx); err != nil {
		return nil, err
	}
	results := make([]dto.EvaluationResult, 0, len(courses))
	for _, course := range courses {
		_ = client.ReviseQuestionnairePattern(ctx, course.Rwid, course.Wjid, course.Msid)
		topicData, err := client.QuestionnaireTopic(ctx, course)
		if err != nil || topicData == nil {
			results = append(results, dto.EvaluationResult{Success: false, Message: "无法获取问卷题目", CourseName: course.Kcmc})
			continue
		}
		pjjglist, ok := buildEvaluationResultsPayload(course, topicData)
		if !ok {
			results = append(results, dto.EvaluationResult{Success: false, Message: "问卷没有题目", CourseName: course.Kcmc})
			continue
		}
		resp, err := client.SubmitEvaluation(ctx, pjjglist)
		if err == nil && resp.isSuccess() {
			results = append(results, dto.EvaluationResult{Success: true, Message: "评教成功", CourseName: course.Kcmc})
			continue
		}
		message := "提交失败，可能已评教"
		code := "no-code"
		if resp != nil {
			if text := strings.TrimSpace(resp.messageText()); text != "" {
				message = text
			}
			if text := strings.TrimSpace(resp.codeString()); text != "" {
				code = text
			}
		}
		results = append(results, dto.EvaluationResult{Success: false, Message: "提交失败(code=" + code + "): " + message, CourseName: course.Kcmc})
	}
	return results, nil
}

type evaluationClient struct {
	username string
	service  *Service
}

type evaluationTaskRaw struct {
	Rwid string `json:"rwid"`
	Rwmc string `json:"rwmc"`
}

type evaluationQuestionnaireRaw struct {
	Wjid string `json:"wjid"`
	Wjmc string `json:"wjmc"`
	Rwmc string `json:"rwmc"`
	Msid string `json:"msid"`
}

type evaluationCourseRaw struct {
	Rwid      string  `json:"rwid"`
	Wjid      string  `json:"wjid"`
	Kcdm      string  `json:"kcdm"`
	Kcmc      string  `json:"kcmc"`
	Bpmc      *string `json:"bpmc"`
	Bpdm      *string `json:"bpdm"`
	Pjrdm     *string `json:"pjrdm"`
	Pjrmc     *string `json:"pjrmc"`
	Zdmc      *string `json:"zdmc"`
	Ypjcs     *int    `json:"ypjcs"`
	Xypjcs    *int    `json:"xypjcs"`
	Sxz       *string `json:"sxz"`
	Rwh       *string `json:"rwh"`
	Xnxq      *string `json:"xnxq"`
	Pjlxid    *string `json:"pjlxid"`
	Sfksqbpj  *string `json:"sfksqbpj"`
	Xn        *string `json:"xn"`
	Xq        *string `json:"xq"`
	Yxsfktjst *string `json:"yxsfktjst"`
	Msid      *string `json:"msid"`
}

type evaluationPage[T any] struct {
	List []T `json:"list"`
}

type evaluationEnvelope[T any] struct {
	Code    any    `json:"code"`
	Result  T      `json:"result"`
	Content T      `json:"content"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
}

func (c *evaluationClient) InitSession(ctx context.Context) (bool, error) {
	resp, err := c.do(ctx, http.MethodGet, "https://spoc.buaa.edu.cn/pjxt/cas", nil, nil)
	if err != nil {
		return false, err
	}
	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

func (c *evaluationClient) CurrentXnxq(ctx context.Context) (string, error) {
	var response evaluationEnvelope[[]map[string]any]
	if err := c.postJSON(ctx, "https://spoc.buaa.edu.cn/pjxt/component/queryXnxq", nil, &response); err != nil {
		return "", err
	}
	if len(response.Content) == 0 {
		return "", nil
	}
	xn := evaluationAnyString(response.Content[0]["xn"])
	xq := evaluationAnyString(response.Content[0]["xq"])
	if xn == "" || xq == "" {
		return "", nil
	}
	return xn + xq, nil
}

func (c *evaluationClient) Tasks(ctx context.Context) ([]evaluationTaskRaw, error) {
	query := url.Values{}
	query.Set("yhdm", c.username)
	query.Set("rwmc", "")
	query.Set("sfyp", "0")
	query.Set("pageNum", "1")
	query.Set("pageSize", "10")
	var response evaluationEnvelope[evaluationPage[evaluationTaskRaw]]
	if err := c.getJSON(ctx, "https://spoc.buaa.edu.cn/pjxt/personnelEvaluation/listObtainPersonnelEvaluationTasks?"+query.Encode(), &response); err != nil {
		return nil, err
	}
	return response.Result.List, nil
}

func (c *evaluationClient) Questionnaires(ctx context.Context, rwid string) ([]evaluationQuestionnaireRaw, error) {
	query := url.Values{}
	query.Set("rwid", rwid)
	query.Set("sfyp", "0")
	query.Set("pageNum", "1")
	query.Set("pageSize", "999")
	var response evaluationEnvelope[[]evaluationQuestionnaireRaw]
	if err := c.getJSON(ctx, "https://spoc.buaa.edu.cn/pjxt/evaluationMethodSix/getQuestionnaireListToTask?"+query.Encode(), &response); err != nil {
		return nil, err
	}
	return response.Result, nil
}

func (c *evaluationClient) Courses(ctx context.Context, rwid, wjid, xnxq, msid, sfyp string) ([]evaluationCourseRaw, error) {
	_ = c.ReviseQuestionnairePattern(ctx, rwid, wjid, msid)
	query := url.Values{}
	query.Set("sfyp", sfyp)
	query.Set("wjid", wjid)
	query.Set("xnxq", xnxq)
	query.Set("pageNum", "1")
	query.Set("pageSize", "999")
	var response evaluationEnvelope[[]evaluationCourseRaw]
	if err := c.getJSON(ctx, "https://spoc.buaa.edu.cn/pjxt/evaluationMethodSix/getRequiredReviewsData?"+query.Encode(), &response); err != nil {
		return nil, err
	}
	for i := range response.Result {
		response.Result[i].Msid = &msid
	}
	return response.Result, nil
}

func (c *evaluationClient) ReviseQuestionnairePattern(ctx context.Context, rwid, wjid, msid string) error {
	if strings.TrimSpace(msid) == "" {
		msid = "1"
	}
	return c.postJSON(ctx, "https://spoc.buaa.edu.cn/pjxt/evaluationMethodSix/reviseQuestionnairePattern", map[string]any{
		"rwid": rwid,
		"wjid": wjid,
		"msid": msid,
	}, nil)
}

func (c *evaluationClient) QuestionnaireTopic(ctx context.Context, course dto.EvaluationCourse) (map[string]any, error) {
	query := url.Values{}
	query.Set("id", "")
	query.Set("rwid", course.Rwid)
	query.Set("wjid", course.Wjid)
	query.Set("zdmc", evaluationStringValue(course.Zdmc, "STID"))
	query.Set("ypjcs", strconv.Itoa(evaluationIntValue(course.Ypjcs, 0)))
	query.Set("xypjcs", strconv.Itoa(evaluationIntValue(course.Xypjcs, 1)))
	query.Set("sxz", evaluationStringValue(course.Sxz, ""))
	query.Set("pjrdm", evaluationStringValue(course.Pjrdm, ""))
	query.Set("pjrmc", evaluationStringValue(course.Pjrmc, ""))
	query.Set("bpdm", evaluationStringValue(course.Bpdm, ""))
	query.Set("bpmc", course.Bpmc)
	query.Set("kcdm", course.Kcdm)
	query.Set("kcmc", course.Kcmc)
	query.Set("rwh", evaluationStringValue(course.Rwh, ""))
	query.Set("xn", evaluationStringValue(course.Xn, ""))
	query.Set("xq", evaluationStringValue(course.Xq, ""))
	query.Set("xnxq", evaluationStringValue(course.Xnxq, ""))
	query.Set("pjlxid", evaluationStringValue(course.Pjlxid, "2"))
	query.Set("sfksqbpj", evaluationStringValue(course.Sfksqbpj, "1"))
	query.Set("yxsfktjst", evaluationStringValue(course.Yxsfktjst, ""))
	query.Set("yxdm", "")
	var response evaluationEnvelope[[]map[string]any]
	if err := c.getJSON(ctx, "https://spoc.buaa.edu.cn/pjxt/evaluationMethodSix/getQuestionnaireTopic?"+query.Encode(), &response); err != nil {
		return nil, err
	}
	if len(response.Result) == 0 {
		return nil, nil
	}
	return response.Result[0], nil
}

func (c *evaluationClient) SubmitEvaluation(ctx context.Context, pjjglist []map[string]any) (*evaluationEnvelope[any], error) {
	payload := map[string]any{
		"pjidlist": []any{},
		"pjjglist": pjjglist,
		"pjzt":     "1",
	}
	var response evaluationEnvelope[any]
	if err := c.postJSON(ctx, "https://spoc.buaa.edu.cn/pjxt/evaluationMethodSix/submitSaveEvaluation", payload, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *evaluationClient) getJSON(ctx context.Context, target string, out any) error {
	resp, err := c.do(ctx, http.MethodGet, target, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return decodeEvaluationJSON(resp, raw, out)
}

func (c *evaluationClient) postJSON(ctx context.Context, target string, payload any, out any) error {
	resp, err := c.do(ctx, http.MethodPost, target, payload, map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return decodeEvaluationJSON(resp, raw, out)
}

func (c *evaluationClient) do(ctx context.Context, method, target string, payload any, headers map[string]string) (*http.Response, error) {
	sessionClient, err := c.service.clients.Get(c.username)
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.service.clients.UpstreamURL(target), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := sessionClient.Client.Do(req)
	if err != nil {
		return nil, upstreamRequestError(err, ErrFeatureUpstream, "evaluation_timeout")
	}
	return resp, nil
}

func decodeEvaluationJSON(resp *http.Response, raw []byte, out any) error {
	if isSessionExpired(resp, raw) {
		return ErrFeatureUnauthenticated
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrFeatureUpstream
	}
	if out == nil || len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return ErrFeatureUpstream
	}
	return nil
}

func mergeEvaluationCourses(courseMap map[string]dto.EvaluationCourse, order *[]string, courses []evaluationCourseRaw, rwid, wjid, xnxq, msid string, isEvaluated bool) {
	for _, raw := range courses {
		key := rwid + "_" + wjid + "_" + raw.Kcdm + "_" + evaluationStringForKey(raw.Bpdm)
		existing, existed := courseMap[key]
		if !existed {
			*order = append(*order, key)
		}
		bpmc := evaluationFirstNonBlank(evaluationStringValue(raw.Bpmc, ""), existing.Bpmc, "未知教师")
		courseMap[key] = dto.EvaluationCourse{
			ID:          key,
			Kcmc:        raw.Kcmc,
			Bpmc:        bpmc,
			IsEvaluated: isEvaluated || existing.IsEvaluated,
			Rwid:        rwid,
			Wjid:        wjid,
			Kcdm:        raw.Kcdm,
			Bpdm:        evaluationFirstPtr(raw.Bpdm, existing.Bpdm),
			Pjrdm:       evaluationFirstPtr(raw.Pjrdm, existing.Pjrdm),
			Pjrmc:       evaluationFirstPtr(raw.Pjrmc, existing.Pjrmc),
			Xnxq:        stringPtrNonBlank(xnxq),
			Msid:        msid,
			Zdmc:        evaluationFirstPtr(raw.Zdmc, existing.Zdmc, stringPtrNonBlank("STID")),
			Ypjcs:       evaluationFirstIntPtr(raw.Ypjcs, existing.Ypjcs, intPtr(0)),
			Xypjcs:      evaluationFirstIntPtr(raw.Xypjcs, existing.Xypjcs, intPtr(1)),
			Sxz:         evaluationFirstPtr(raw.Sxz, existing.Sxz),
			Rwh:         evaluationFirstPtr(raw.Rwh, existing.Rwh),
			Xn:          evaluationFirstPtr(raw.Xn, existing.Xn),
			Xq:          evaluationFirstPtr(raw.Xq, existing.Xq),
			Pjlxid:      evaluationFirstPtr(raw.Pjlxid, existing.Pjlxid, stringPtrNonBlank("2")),
			Sfksqbpj:    evaluationFirstPtr(raw.Sfksqbpj, existing.Sfksqbpj, stringPtrNonBlank("1")),
			Yxsfktjst:   evaluationFirstPtr(raw.Yxsfktjst, existing.Yxsfktjst),
		}
	}
}

func buildEvaluationResultsPayload(course dto.EvaluationCourse, topicData map[string]any) ([]map[string]any, bool) {
	wjEntity := evaluationMap(topicData["pjxtWjWjbReturnEntity"])
	wjzblist := evaluationSlice(wjEntity["wjzblist"])
	allQuestions := []map[string]any{}
	for _, rawZb := range wjzblist {
		zb := evaluationMap(rawZb)
		for _, rawQuestion := range evaluationSlice(zb["tklist"]) {
			if question := evaluationMap(rawQuestion); question != nil {
				allQuestions = append(allQuestions, question)
			}
		}
	}
	if len(allQuestions) == 0 {
		return nil, false
	}
	randomIndex := rand.Intn(len(allQuestions))
	pjmap, hasPjmap := topicData["pjmap"]
	if !hasPjmap {
		pjmap = nil
	}
	pjjglist := []map[string]any{}
	for _, rawPjjg := range evaluationSlice(topicData["pjxtPjjgPjjgckb"]) {
		pjjg := evaluationMap(rawPjjg)
		pjxxlist := []map[string]any{}
		for i, question := range allQuestions {
			tmlx := evaluationAnyStringDefault(question["tmlx"], "1")
			tmxxlist := evaluationSlice(question["tmxxlist"])
			if len(tmxxlist) == 0 {
				continue
			}
			firstOptionID := evaluationMap(tmxxlist[0])["tmxxid"]
			selectedOptionID := firstOptionID
			if i == randomIndex && len(tmxxlist) > 1 {
				selectedOptionID = evaluationMap(tmxxlist[1])["tmxxid"]
			}
			answers := []any{}
			if selectedOptionID != nil {
				answers = append(answers, selectedOptionID)
			}
			wjstctid := any("")
			if tmlx == "6" && firstOptionID != nil {
				wjstctid = firstOptionID
			}
			pjxxlist = append(pjxxlist, map[string]any{
				"sjly":     "1",
				"stlx":     tmlx,
				"wjid":     course.Wjid,
				"wjssrwid": evaluationMapValue(pjjg, "wjssrwid"),
				"wjstctid": wjstctid,
				"wjstid":   evaluationMapValue(question, "tmid"),
				"xxdalist": answers,
			})
		}
		pjjglist = append(pjjglist, map[string]any{
			"bprdm":    evaluationMapValue(pjjg, "bprdm"),
			"bprmc":    evaluationMapValue(pjjg, "bprmc"),
			"kcdm":     evaluationMapValue(pjjg, "kcdm"),
			"kcmc":     evaluationMapValue(pjjg, "kcmc"),
			"pjdf":     93,
			"pjfs":     evaluationMapValueDefault(pjjg, "pjfs", "1"),
			"pjid":     evaluationMapValue(pjjg, "pjid"),
			"pjlx":     evaluationMapValue(pjjg, "pjlx"),
			"pjmap":    pjmap,
			"pjrdm":    evaluationMapValue(pjjg, "pjrdm"),
			"pjrjsdm":  evaluationMapValue(pjjg, "pjrjsdm"),
			"pjrxm":    evaluationMapValue(pjjg, "pjrxm"),
			"pjsx":     1,
			"pjxxlist": pjxxlist,
			"rwh":      evaluationMapValue(pjjg, "rwh"),
			"stzjid":   "xx",
			"wjid":     course.Wjid,
			"wjssrwid": evaluationMapValue(pjjg, "wjssrwid"),
			"wtjjy":    "",
			"xhgs":     nil,
			"xnxq":     evaluationMapValue(pjjg, "xnxq"),
			"sfxxpj":   evaluationMapValueDefault(pjjg, "sfxxpj", "1"),
			"sqzt":     nil,
			"yxfz":     nil,
			"zsxz":     evaluationMapValueDefault(pjjg, "pjrjsdm", ""),
			"sfnm":     "1",
		})
	}
	return pjjglist, true
}

func emptyEvaluationCoursesResponse() dto.EvaluationCoursesResponse {
	return dto.EvaluationCoursesResponse{
		Courses:  []dto.EvaluationCourse{},
		Progress: evaluationProgress(0, 0, 0),
	}
}

func evaluationProgress(total, evaluated, pending int) dto.EvaluationProgress {
	percent := 0
	if total > 0 {
		percent = evaluated * 100 / total
	}
	return dto.EvaluationProgress{
		TotalCourses:     total,
		EvaluatedCourses: evaluated,
		PendingCourses:   pending,
		ProgressPercent:  percent,
		IsCompleted:      pending == 0 && total > 0,
	}
}

func evaluationError(c *fiber.Ctx, err error) error {
	if code, ok := timeoutCode(err); ok {
		return httpx.Error(c, fiber.StatusGatewayTimeout, code)
	}
	if errorsIsUnauth(err) {
		return httpx.Error(c, fiber.StatusUnauthorized, "invalid_token")
	}
	return httpx.Error(c, fiber.StatusInternalServerError, "evaluation_error")
}

func (e evaluationEnvelope[T]) isSuccess() bool {
	code := strings.ToLower(strings.TrimSpace(e.codeString()))
	return code == "200" || code == "0" || code == "success"
}

func (e evaluationEnvelope[T]) codeString() string {
	return evaluationAnyString(e.Code)
}

func (e evaluationEnvelope[T]) messageText() string {
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return e.Msg
}

func evaluationAnyString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case json.Number:
		return typed.String()
	default:
		return strings.Trim(strings.TrimSpace(fmt.Sprint(typed)), `"`)
	}
}

func evaluationAnyStringDefault(value any, fallback string) string {
	if text := evaluationAnyString(value); text != "" {
		return text
	}
	return fallback
}

func evaluationStringValue(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return strings.TrimSpace(*value)
}

func evaluationIntValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func evaluationFirstNonBlank(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

func evaluationStringForKey(value *string) string {
	if value == nil {
		return "null"
	}
	return *value
}

func evaluationFirstPtr(values ...*string) *string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			text := strings.TrimSpace(*value)
			return &text
		}
	}
	return nil
}

func evaluationFirstIntPtr(values ...*int) *int {
	for _, value := range values {
		if value != nil {
			copy := *value
			return &copy
		}
	}
	return nil
}

func intPtr(value int) *int {
	return &value
}

func evaluationMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func evaluationSlice(value any) []any {
	if typed, ok := value.([]any); ok {
		return typed
	}
	return nil
}

func evaluationMapValue(values map[string]any, key string) any {
	if values == nil {
		return nil
	}
	if value, ok := values[key]; ok {
		return value
	}
	return nil
}

func evaluationMapValueDefault(values map[string]any, key string, fallback any) any {
	if value := evaluationMapValue(values, key); value != nil {
		return value
	}
	return fallback
}
