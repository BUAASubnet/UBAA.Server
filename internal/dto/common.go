package dto

type ErrorResponse struct {
	Error ErrorDetails `json:"error"`
}

type ErrorDetails struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type UserData struct {
	Name     string `json:"name"`
	SchoolID string `json:"schoolid"`
}

type UserInfo struct {
	IDCardType     *string `json:"idCardType"`
	IDCardTypeName *string `json:"idCardTypeName"`
	Phone          *string `json:"phone"`
	SchoolID       *string `json:"schoolid"`
	Name           *string `json:"name"`
	IDCardNumber   *string `json:"idCardNumber"`
	Email          *string `json:"email"`
	Username       *string `json:"username"`
}

type UserInfoResponse struct {
	Code int       `json:"code"`
	Data *UserInfo `json:"data"`
}

type UserStatusResponse struct {
	Code int            `json:"code"`
	Data UserStatusData `json:"data"`
}

type UserStatusData struct {
	Name     string `json:"name"`
	SchoolID string `json:"schoolid"`
	Username string `json:"username"`
}

type HealthCheckResponse struct {
	Status     string            `json:"status"`
	InstanceID string            `json:"instanceId"`
	Checks     map[string]string `json:"checks"`
}
