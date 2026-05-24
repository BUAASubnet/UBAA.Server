# Compatibility Inventory

Source of truth: UBAA `upstream/dev` at `885db44`.

## Anonymous Endpoints

- `GET /`
- `GET /metrics`
- `GET /health/live`
- `GET /health/ready`
- `GET /api/v1/app/version`
- `GET /api/v1/app/announcement`
- `POST /api/v1/auth/preload`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/login-stats`
- `POST /api/v1/auth/refresh`
- `GET /api/v1/auth/status`
- `POST /api/v1/auth/logout`
- `GET /api/v1/auth/captcha/{captchaId}`

## JWT-Protected Endpoints

- `GET /api/v1/user/info`
- `GET /api/v1/schedule/terms`
- `GET /api/v1/schedule/weeks`
- `GET /api/v1/schedule/week`
- `GET /api/v1/schedule/today`
- `GET /api/v1/exam/list`
- `GET /api/v1/grade/list`
- `GET /api/v1/signin/today`
- `POST /api/v1/signin/do`
- `GET /api/v1/classroom/query`
- `GET /api/v1/bykc/profile`
- `GET /api/v1/bykc/courses`
- `GET /api/v1/bykc/statistics`
- `POST /api/v1/bykc/courses/{courseId}/select`
- `DELETE /api/v1/bykc/courses/{courseId}/select`
- `GET /api/v1/bykc/courses/chosen`
- `GET /api/v1/bykc/courses/{courseId}`
- `POST /api/v1/bykc/courses/{courseId}/sign`
- `GET /api/v1/cgyy/sites`
- `GET /api/v1/cgyy/purpose-types`
- `GET /api/v1/cgyy/day-info`
- `POST /api/v1/cgyy/reservations`
- `GET /api/v1/cgyy/orders/lock-code`
- `GET /api/v1/cgyy/orders`
- `GET /api/v1/cgyy/orders/{orderId}`
- `POST /api/v1/cgyy/orders/{orderId}/cancel`
- `GET /api/v1/evaluation/list`
- `POST /api/v1/evaluation/submit`
- `GET /api/v1/spoc/assignments`
- `GET /api/v1/spoc/assignments/{assignmentId}`
- `GET /api/v1/judge/assignments`
- `GET /api/v1/judge/courses/{courseId}/assignments/{assignmentId}`
- `POST /api/v1/judge/assignment-details`
- `GET /api/v1/libbook/libraries`
- `GET /api/v1/libbook/areas`
- `GET /api/v1/libbook/areas/{areaId}`
- `GET /api/v1/libbook/areas/{areaId}/seats`
- `POST /api/v1/libbook/bookings`
- `GET /api/v1/libbook/reservations`
- `POST /api/v1/libbook/bookings/{bookingId}/cancel`
- `GET /api/v1/ygdk/overview`
- `GET /api/v1/ygdk/records`
- `POST /api/v1/ygdk/records`

## Port Status

Real upstream behavior implemented:

- Auth CAS/SSO session management
- App version and announcement
- Health
- User info from local authenticated session
- Schedule
- Exam
- Grade
- Signin
- Classroom
- SPOC
- Judge
- Evaluation
- LibBook
- CGYY
- BYKC
- YGDK

Compatibility tests cover route contracts, crypto/signing helpers, parsers, DTO mapping, WebVPN URL conversion, metrics output, iclass SSO loginName resolution, and sampled upstream payload behavior.
