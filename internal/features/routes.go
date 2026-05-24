package features

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/BUAASubnet/UBAA.Server/internal/auth"
	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"github.com/BUAASubnet/UBAA.Server/internal/httpx"
	"github.com/gofiber/fiber/v2"
)

func RegisterRoutes(api fiber.Router, service *Service) {
	if service == nil {
		service = NewService(nil)
	}
	registerUser(api)
	registerSchedule(api, service)
	registerExam(api, service)
	registerGrade(api, service)
	registerSignin(api, service)
	registerClassroom(api, service)
	registerBykc(api, service)
	registerCgyy(api, service)
	registerEvaluation(api, service)
	registerSpoc(api, service)
	registerJudge(api, service)
	registerLibBook(api, service)
	registerYgdk(api, service)
}

func registerUser(api fiber.Router) {
	group := api.Group("/user")
	group.Get("/info", func(c *fiber.Ctx) error {
		session, err := auth.RequireSession(c)
		if err != nil {
			return err
		}
		schoolID := session.UserData.SchoolID
		name := session.UserData.Name
		return c.Status(fiber.StatusOK).JSON(dto.UserInfoResponse{
			Code: 0,
			Data: &dto.UserInfo{
				SchoolID: &schoolID,
				Name:     &name,
				Username: &session.Username,
			},
		})
	})
}

func registerSchedule(api fiber.Router, service *Service) {
	group := api.Group("/schedule")
	group.Get("/terms", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		terms, err := service.FetchTerms(context.Background(), session.Username)
		if err != nil {
			return featureError(c, err, "schedule_error")
		}
		return c.Status(fiber.StatusOK).JSON(terms)
	})
	group.Get("/weeks", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		termCode := strings.TrimSpace(c.Query("termCode"))
		if termCode == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		weeks, err := service.FetchWeeks(context.Background(), session.Username, termCode)
		if err != nil {
			return featureError(c, err, "schedule_error")
		}
		return c.Status(fiber.StatusOK).JSON(weeks)
	})
	group.Get("/week", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		termCode := strings.TrimSpace(c.Query("termCode"))
		week, err := strconv.Atoi(c.Query("week"))
		if termCode == "" || err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		schedule, err := service.FetchWeeklySchedule(context.Background(), session.Username, termCode, week)
		if err != nil {
			return featureError(c, err, "schedule_error")
		}
		return c.Status(fiber.StatusOK).JSON(schedule)
	})
	group.Get("/today", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		today, err := service.FetchTodaySchedule(context.Background(), session.Username)
		if err != nil {
			return featureError(c, err, "schedule_error")
		}
		return c.Status(fiber.StatusOK).JSON(today)
	})
}

func registerExam(api fiber.Router, service *Service) {
	api.Get("/exam/list", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		termCode := strings.TrimSpace(c.Query("termCode"))
		if termCode == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		exams, err := service.FetchExams(context.Background(), session.Username, termCode)
		if err != nil {
			return featureError(c, err, "exam_error")
		}
		return c.Status(fiber.StatusOK).JSON(exams)
	})
}

func registerGrade(api fiber.Router, service *Service) {
	api.Get("/grade/list", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		termCode := strings.TrimSpace(c.Query("termCode"))
		if termCode == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		grades, err := service.FetchGrades(context.Background(), session.Username, termCode)
		if err != nil {
			return featureError(c, err, "grade_error")
		}
		return c.Status(fiber.StatusOK).JSON(grades)
	})
}

func registerSignin(api fiber.Router, service *Service) {
	group := api.Group("/signin")
	group.Get("/today", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		response, err := service.GetTodaySigninClasses(context.Background(), session.UserData.SchoolID)
		if err != nil {
			return featureError(c, err, "signin_load_failed")
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Post("/do", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		courseID := strings.TrimSpace(c.Query("courseId"))
		if courseID == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.PerformSignin(context.Background(), session.UserData.SchoolID, courseID)
		if err != nil {
			return featureError(c, err, "signin_failed")
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
}

func registerClassroom(api fiber.Router, service *Service) {
	api.Get("/classroom/query", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		date := strings.TrimSpace(c.Query("date"))
		if date == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		xqid := queryInt(c, "xqid", 1)
		result, err := service.QueryClassroom(context.Background(), session.Username, xqid, date)
		if err != nil {
			return featureError(c, err, "classroom_query_failed")
		}
		return c.Status(fiber.StatusOK).JSON(result)
	})
}

func registerBykc(api fiber.Router, service *Service) {
	group := api.Group("/bykc")
	group.Get("/profile", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		response, err := service.GetBykcUserProfile(context.Background(), session.Username)
		if err != nil {
			return bykcError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/courses", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		page := queryInt(c, "page", 1)
		size := queryInt(c, "size", 20)
		if page < 1 || size < 1 || size > 500 {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.GetBykcCourses(context.Background(), session.Username, page, size, c.QueryBool("all", false))
		if err != nil {
			return bykcError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/statistics", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		response, err := service.GetBykcStatistics(context.Background(), session.Username)
		if err != nil {
			return bykcError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Post("/courses/:courseId/select", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		courseID, err := strconv.ParseInt(c.Params("courseId"), 10, 64)
		if err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		message, err := service.SelectBykcCourse(context.Background(), session.Username, courseID)
		if err != nil {
			code := "select_failed"
			if strings.Contains(err.Error(), "重复报名") {
				code = "already_selected"
			} else if strings.Contains(err.Error(), "人数已满") {
				code = "course_full"
			} else if strings.Contains(err.Error(), "不可选择") {
				code = "course_not_selectable"
			}
			return httpx.Error(c, fiber.StatusConflict, code)
		}
		return c.Status(fiber.StatusOK).JSON(dto.BykcSuccessResponse{Message: message})
	})
	group.Delete("/courses/:courseId/select", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		courseID, err := strconv.ParseInt(c.Params("courseId"), 10, 64)
		if err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		message, err := service.DeselectBykcCourse(context.Background(), session.Username, courseID)
		if err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "deselect_failed")
		}
		return c.Status(fiber.StatusOK).JSON(dto.BykcSuccessResponse{Message: message})
	})
	group.Get("/courses/chosen", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		response, err := service.GetBykcChosenCourses(context.Background(), session.Username)
		if err != nil {
			return bykcError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/courses/:courseId", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		courseID, err := strconv.ParseInt(c.Params("courseId"), 10, 64)
		if err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.GetBykcCourseDetail(context.Background(), session.Username, courseID)
		if err != nil {
			return bykcError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Post("/courses/:courseId/sign", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		courseID, err := strconv.ParseInt(c.Params("courseId"), 10, 64)
		if err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		var request dto.BykcSignRequest
		if err := c.BodyParser(&request); err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		message, err := service.SignBykcCourse(context.Background(), session.Username, courseID, request)
		if err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "sign_failed")
		}
		return c.Status(fiber.StatusOK).JSON(dto.BykcSuccessResponse{Message: message})
	})
}

func registerCgyy(api fiber.Router, service *Service) {
	group := api.Group("/cgyy")
	group.Get("/sites", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		response, err := service.GetCgyyVenueSites(context.Background(), session.Username)
		if err != nil {
			return cgyyError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/purpose-types", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		response, err := service.GetCgyyPurposeTypes(context.Background(), session.Username)
		if err != nil {
			return cgyyError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/day-info", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		venueSiteID, err := strconv.Atoi(c.Query("venueSiteId"))
		date := strings.TrimSpace(c.Query("date"))
		if err != nil || date == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.GetCgyyDayInfo(context.Background(), session.Username, venueSiteID, date)
		if err != nil {
			return cgyyError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Post("/reservations", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		var request dto.CgyyReservationSubmitRequest
		if err := c.BodyParser(&request); err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.SubmitCgyyReservation(context.Background(), session.Username, request)
		if err != nil {
			return cgyyError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/orders/lock-code", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		response, err := service.GetCgyyLockCode(context.Background(), session.Username)
		if err != nil {
			return cgyyError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/orders", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		size := queryInt(c, "size", 20)
		page := queryInt(c, "page", 0)
		response, err := service.GetCgyyOrders(context.Background(), session.Username, page, size)
		if err != nil {
			return cgyyError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/orders/:orderId", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		orderID, err := strconv.Atoi(c.Params("orderId"))
		if err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.GetCgyyOrderDetail(context.Background(), session.Username, orderID)
		if err != nil {
			return cgyyError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Post("/orders/:orderId/cancel", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		orderID, err := strconv.Atoi(c.Params("orderId"))
		if err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.CancelCgyyOrder(context.Background(), session.Username, orderID)
		if err != nil {
			return cgyyError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
}

func registerEvaluation(api fiber.Router, service *Service) {
	group := api.Group("/evaluation")
	group.Get("/list", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		response, err := service.GetEvaluationCourses(context.Background(), session.Username)
		if err != nil {
			return evaluationError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Post("/submit", func(c *fiber.Ctx) error {
		var courses []dto.EvaluationCourse
		if err := c.BodyParser(&courses); err != nil || len(courses) == 0 {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		session := auth.CurrentSession(c)
		results, err := service.AutoEvaluate(context.Background(), session.Username, courses)
		if err != nil {
			return evaluationError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(results)
	})
}

func registerSpoc(api fiber.Router, service *Service) {
	group := api.Group("/spoc")
	group.Get("/assignments", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		response, err := service.GetSpocAssignments(context.Background(), session.Username)
		if err != nil {
			return spocError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/assignments/:assignmentId", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		assignmentID := strings.TrimSpace(c.Params("assignmentId"))
		if assignmentID == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.GetSpocAssignmentDetail(context.Background(), session.Username, assignmentID)
		if err != nil {
			return spocError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
}

func registerJudge(api fiber.Router, service *Service) {
	group := api.Group("/judge")
	group.Get("/assignments", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		includeExpired := c.QueryBool("includeExpired", false)
		skipped := map[string]bool{}
		for _, value := range c.Context().QueryArgs().PeekMulti("skipCourseId") {
			text := strings.TrimSpace(string(value))
			if text != "" {
				skipped[text] = true
			}
		}
		response, err := service.GetJudgeAssignments(context.Background(), session.Username, includeExpired, skipped)
		if err != nil {
			return judgeError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/courses/:courseId/assignments/:assignmentId", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		courseID := strings.TrimSpace(c.Params("courseId"))
		assignmentID := strings.TrimSpace(c.Params("assignmentId"))
		if courseID == "" || assignmentID == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.GetJudgeAssignmentDetail(context.Background(), session.Username, courseID, assignmentID)
		if err != nil {
			return judgeError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Post("/assignment-details", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		var request dto.JudgeAssignmentDetailsRequest
		if err := c.BodyParser(&request); err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.GetJudgeAssignmentDetails(context.Background(), session.Username, request.Keys)
		if err != nil {
			return judgeError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
}

func registerLibBook(api fiber.Router, service *Service) {
	group := api.Group("/libbook")
	group.Get("/libraries", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		day := strings.TrimSpace(c.Query("day"))
		if day == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.GetLibBookLibraries(context.Background(), session.Username, day)
		if err != nil {
			return libBookError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/areas", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		premisesID := strings.TrimSpace(c.Query("premisesId"))
		storeyID := strings.TrimSpace(c.Query("storeyId"))
		day := strings.TrimSpace(c.Query("day"))
		if premisesID == "" || day == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.GetLibBookAreas(context.Background(), session.Username, premisesID, storeyID, day)
		if err != nil {
			return libBookError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/areas/:areaId", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		areaID := strings.TrimSpace(c.Params("areaId"))
		if areaID == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.GetLibBookAreaDetail(context.Background(), session.Username, areaID)
		if err != nil {
			return libBookError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/areas/:areaId/seats", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		areaID := strings.TrimSpace(c.Params("areaId"))
		day := strings.TrimSpace(c.Query("day"))
		startTime := strings.TrimSpace(c.Query("startTime"))
		endTime := strings.TrimSpace(c.Query("endTime"))
		if areaID == "" || day == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.GetLibBookSeats(context.Background(), session.Username, areaID, day, startTime, endTime)
		if err != nil {
			return libBookError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Post("/bookings", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		var request dto.LibBookReserveRequest
		if err := c.BodyParser(&request); err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.ReserveLibBookSeat(context.Background(), session.Username, request)
		if err != nil {
			return libBookError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/reservations", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		page := queryInt(c, "page", 1)
		limit := queryInt(c, "limit", 20)
		response, err := service.GetLibBookBookings(context.Background(), session.Username, page, limit)
		if err != nil {
			return libBookError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Post("/bookings/:bookingId/cancel", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		bookingID := strings.TrimSpace(c.Params("bookingId"))
		if bookingID == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.CancelLibBookBooking(context.Background(), session.Username, bookingID)
		if err != nil {
			return libBookError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
}

func registerYgdk(api fiber.Router, service *Service) {
	group := api.Group("/ygdk")
	group.Get("/overview", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		response, err := service.GetYgdkOverview(context.Background(), session.Username)
		if err != nil {
			return ygdkError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Get("/records", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		page := queryInt(c, "page", 1)
		size := queryInt(c, "size", 20)
		response, err := service.GetYgdkRecords(context.Background(), session.Username, page, size)
		if err != nil {
			return ygdkError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
	group.Post("/records", func(c *fiber.Ctx) error {
		session := auth.CurrentSession(c)
		request, err := parseYgdkClockinRequest(c)
		if err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
		}
		response, err := service.SubmitYgdkClockin(context.Background(), session.Username, request)
		if err != nil {
			return ygdkError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})
}

func queryInt(c *fiber.Ctx, name string, fallback int) int {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func featureError(c *fiber.Ctx, err error, fallbackCode string) error {
	if code, ok := timeoutCode(err); ok {
		return httpx.Error(c, fiber.StatusGatewayTimeout, code)
	}
	switch {
	case errors.Is(err, ErrFeatureUnauthenticated):
		return httpx.Error(c, fiber.StatusUnauthorized, "invalid_token")
	case errors.Is(err, ErrUnsupportedPortal):
		return httpx.Error(c, fiber.StatusNotImplemented, "unsupported_portal")
	default:
		if fallbackCode == "classroom_query_failed" {
			return httpx.Error(c, fiber.StatusInternalServerError, fallbackCode)
		}
		return httpx.Error(c, fiber.StatusBadGateway, fallbackCode)
	}
}

func spocError(c *fiber.Ctx, err error) error {
	if code, ok := timeoutCode(err); ok {
		return httpx.Error(c, fiber.StatusGatewayTimeout, code)
	}
	switch {
	case errors.Is(err, ErrSpocAuth):
		return httpx.Error(c, fiber.StatusBadGateway, "spoc_auth_failed")
	default:
		return featureError(c, err, "spoc_error")
	}
}

func judgeError(c *fiber.Ctx, err error) error {
	if code, ok := timeoutCode(err); ok {
		return httpx.Error(c, fiber.StatusGatewayTimeout, code)
	}
	switch {
	case errors.Is(err, ErrJudgeAuth):
		return httpx.Error(c, fiber.StatusBadGateway, "judge_auth_failed")
	default:
		return featureError(c, err, "judge_error")
	}
}
