package httpx

import (
	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"github.com/gofiber/fiber/v2"
)

func UserFacingMessage(code string, fallback ...string) string {
	switch code {
	case "invalid_request":
		return "请求参数不正确，请检查后重试"
	case "invalid_credentials":
		return "账号或密码错误，请重试"
	case "invalid_refresh_token", "invalid_token", "unauthenticated":
		return "登录状态已失效，请重新登录"
	case "auth_upstream_timeout":
		return "认证服务响应超时，请稍后重试"
	case "auth_lock_timeout":
		return "当前登录请求较多，请稍后重试"
	case "captcha_not_found":
		return "验证码已失效，请刷新后重试"
	case "captcha_error":
		return "验证码处理失败，请稍后重试"
	case "missing_client_version":
		return "缺少客户端版本信息"
	case "internal_server_error":
		return "服务器开小差了，请稍后再试"
	case "unsupported_portal":
		return "当前账号类型暂不支持该功能"
	case "already_selected":
		return "您已报名过该课程，请勿重复报名"
	case "course_full":
		return "该课程人数已满，请选择其他课程"
	case "course_not_selectable":
		return "该课程当前不可报名"
	case "select_failed":
		return "报名失败，请稍后重试"
	case "deselect_failed":
		return "退选失败，请稍后重试"
	case "sign_failed", "signin_failed":
		return "签到失败，请稍后重试"
	case "signin_load_failed":
		return "签到信息加载失败，请稍后重试"
	case "bykc_error":
		return "博雅课程服务暂时不可用，请稍后重试"
	case "bykc_timeout":
		return "博雅课程服务响应超时，请稍后重试"
	case "cgyy_error":
		return "研讨室服务暂时不可用，请稍后重试"
	case "cgyy_timeout":
		return "研讨室服务响应超时，请稍后重试"
	case "reservation_invalid", "reservation_token_missing":
		return "预约信息已失效，请刷新后重试"
	case "day_info_failed":
		return "获取研讨室可用信息失败，请稍后重试"
	case "spoc_auth_failed":
		return "SPOC 登录状态异常，请重新登录后重试"
	case "spoc_error":
		return "SPOC 服务暂时不可用，请稍后重试"
	case "spoc_timeout":
		return "SPOC 服务响应超时，请稍后重试"
	case "judge_auth_failed":
		return "希冀登录状态异常，请重新登录后重试"
	case "judge_not_found":
		return "希冀作业不存在或无权限访问，请刷新后重试"
	case "judge_error":
		return "希冀服务暂时不可用，请稍后重试"
	case "judge_timeout":
		return "希冀服务响应超时，请稍后重试"
	case "libbook_auth_failed":
		return "图书馆登录状态异常，请重新登录后重试"
	case "libbook_error":
		return "图书馆预约服务暂时不可用，请稍后重试"
	case "libbook_timeout":
		return "图书馆预约服务响应超时，请稍后重试"
	case "libbook_seat_unavailable":
		return "该座位已不可预约，请刷新后重试"
	case "libbook_not_found":
		return "图书馆预约记录不存在或已失效，请刷新后重试"
	case "ygdk_error":
		return "阳光打卡服务暂时不可用，请稍后重试"
	case "ygdk_timeout":
		return "阳光打卡服务响应超时，请稍后重试"
	case "schedule_error":
		return "课表查询失败，请稍后重试"
	case "exam_error":
		return "考试信息查询失败，请稍后重试"
	case "grade_error":
		return "成绩查询失败，请稍后重试"
	case "user_info_failed":
		return "用户信息查询失败，请稍后重试"
	case "classroom_query_failed":
		return "空闲教室查询失败，请稍后重试"
	case "evaluation_error":
		return "评教服务暂时不可用，请稍后重试"
	case "evaluation_timeout":
		return "评教服务响应超时，请稍后重试"
	default:
		if len(fallback) > 0 && fallback[0] != "" {
			return fallback[0]
		}
		return "请求失败，请稍后重试"
	}
}

func Error(c *fiber.Ctx, status int, code string, fallback ...string) error {
	return c.Status(status).JSON(dto.ErrorResponse{
		Error: dto.ErrorDetails{
			Code:    code,
			Message: UserFacingMessage(code, fallback...),
		},
	})
}

func FiberErrorHandler(c *fiber.Ctx, err error) error {
	if e, ok := err.(*fiber.Error); ok {
		if e.Code == fiber.StatusNotFound {
			return Error(c, fiber.StatusNotFound, "invalid_request")
		}
		return Error(c, e.Code, "internal_server_error")
	}
	return Error(c, fiber.StatusInternalServerError, "internal_server_error")
}
