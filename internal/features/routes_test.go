package features

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"github.com/gofiber/fiber/v2"
)

func TestSpocAndJudgeAuthErrorsMatchKtorRouteCodes(t *testing.T) {
	tests := []struct {
		name string
		call func(*fiber.Ctx, error) error
		err  error
		code string
	}{
		{name: "spoc", call: spocError, err: ErrSpocAuth, code: "spoc_auth_failed"},
		{name: "judge", call: judgeError, err: ErrJudgeAuth, code: "judge_auth_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c *fiber.Ctx) error {
				return test.call(c, test.err)
			})
			resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusBadGateway {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			var body dto.ErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != test.code {
				t.Fatalf("code = %q, want %q", body.Error.Code, test.code)
			}
		})
	}
}

func TestFeatureTimeoutErrorsMatchKtorGatewayTimeout(t *testing.T) {
	tests := []struct {
		name string
		call func(*fiber.Ctx, error) error
		err  error
		code string
	}{
		{name: "generic", call: func(c *fiber.Ctx, err error) error { return featureError(c, err, "schedule_error") }, err: timeoutError("upstream_timeout"), code: "upstream_timeout"},
		{name: "spoc", call: spocError, err: timeoutError("spoc_timeout"), code: "spoc_timeout"},
		{name: "judge", call: judgeError, err: timeoutError("judge_timeout"), code: "judge_timeout"},
		{name: "bykc", call: bykcError, err: timeoutError("bykc_timeout"), code: "bykc_timeout"},
		{name: "cgyy", call: cgyyError, err: timeoutError("cgyy_timeout"), code: "cgyy_timeout"},
		{name: "libbook", call: libBookError, err: timeoutError("libbook_timeout"), code: "libbook_timeout"},
		{name: "ygdk", call: ygdkError, err: timeoutError("ygdk_timeout"), code: "ygdk_timeout"},
		{name: "evaluation", call: evaluationError, err: timeoutError("evaluation_timeout"), code: "evaluation_timeout"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c *fiber.Ctx) error {
				return test.call(c, test.err)
			})
			resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusGatewayTimeout {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			var body dto.ErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != test.code {
				t.Fatalf("code = %q, want %q", body.Error.Code, test.code)
			}
		})
	}
}
