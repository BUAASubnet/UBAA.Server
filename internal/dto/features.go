package dto

import "encoding/json"

type Term struct {
	ItemCode  string `json:"itemCode"`
	ItemName  string `json:"itemName"`
	Selected  bool   `json:"selected"`
	ItemIndex int    `json:"itemIndex"`
}

type Week struct {
	StartDate    string `json:"startDate"`
	EndDate      string `json:"endDate"`
	Term         string `json:"term"`
	CurWeek      bool   `json:"curWeek"`
	SerialNumber int    `json:"serialNumber"`
	Name         string `json:"name"`
}

type CourseClass struct {
	CourseCode       string  `json:"courseCode"`
	CourseName       string  `json:"courseName"`
	CourseSerialNo   *string `json:"courseSerialNo"`
	Credit           *string `json:"credit"`
	BeginTime        *string `json:"beginTime"`
	EndTime          *string `json:"endTime"`
	BeginSection     *int    `json:"beginSection"`
	EndSection       *int    `json:"endSection"`
	PlaceName        *string `json:"placeName"`
	WeeksAndTeachers *string `json:"weeksAndTeachers"`
	TeachingTarget   *string `json:"teachingTarget"`
	Color            *string `json:"color"`
	DayOfWeek        *int    `json:"dayOfWeek"`
}

type WeeklySchedule struct {
	ArrangedList []CourseClass `json:"arrangedList"`
	Code         string        `json:"code"`
	Name         string        `json:"name"`
}

type TodayClass struct {
	BizName   string  `json:"bizName"`
	Place     *string `json:"place"`
	Time      *string `json:"time"`
	ShortName *string `json:"shortName"`
}

type TermResponse struct {
	Datas []Term  `json:"datas"`
	Code  string  `json:"code"`
	Msg   *string `json:"msg"`
}

type WeekResponse struct {
	Datas []Week  `json:"datas"`
	Code  string  `json:"code"`
	Msg   *string `json:"msg"`
}

type WeeklyScheduleResponse struct {
	Datas WeeklySchedule `json:"datas"`
	Code  string         `json:"code"`
	Msg   *string        `json:"msg"`
}

type TodayScheduleResponse struct {
	Datas []TodayClass `json:"datas"`
	Code  string       `json:"code"`
	Msg   *string      `json:"msg"`
}

type ExamArrangementData struct {
	StuInfo     *ExamStudentInfo `json:"stuInfo"`
	Arranged    []Exam           `json:"arranged"`
	NotArranged []Exam           `json:"notArranged"`
}

type ExamStudentInfo struct {
	Name       *string `json:"name"`
	StudentID  *string `json:"studentId"`
	Department *string `json:"department"`
	Major      *string `json:"major"`
	Grade      *string `json:"grade"`
}

type Exam struct {
	CourseName          string  `json:"courseName"`
	CourseNo            *string `json:"courseNo"`
	ExamTimeDescription *string `json:"examTimeDescription"`
	ExamDate            *string `json:"examDate"`
	StartTime           *string `json:"startTime"`
	EndTime             *string `json:"endTime"`
	ExamPlace           *string `json:"examPlace"`
	ExamSeatNo          *string `json:"examSeatNo"`
	Week                *int    `json:"week"`
	ExamStatus          *int    `json:"examStatus"`
	ExamType            *string `json:"examType"`
	TaskID              *string `json:"taskId"`
}

type GradeData struct {
	TermCode string  `json:"termCode"`
	Grades   []Grade `json:"grades"`
}

type Grade struct {
	ID                    *string  `json:"WID"`
	TermCode              *string  `json:"XNXQDM"`
	TermName              *string  `json:"XNXQDM_DISPLAY"`
	CourseName            *string  `json:"KCM"`
	CourseCode            *string  `json:"KCH"`
	ReplacementCourseName *string  `json:"TDKCM"`
	ReplacementCourseCode *string  `json:"TDKCH"`
	Credit                *float64 `json:"XF"`
	Score                 *string  `json:"XSZCJ"`
	GradePoint            *string  `json:"JD"`
	CourseCategory        *string  `json:"KCLBDM_DISPLAY"`
	CourseGroup           *string  `json:"XGXKLBDM_DISPLAY"`
	CourseAttribute       *string  `json:"KCXZDM_DISPLAY"`
	ExamType              *string  `json:"KSLXDM_DISPLAY"`
	ExamAttempt           *string  `json:"CXCKDM_DISPLAY"`
	Passed                *string  `json:"SFJG_DISPLAY"`
	Effective             *string  `json:"SFYX_DISPLAY"`
	RecognitionType       *string  `json:"CJRDFSDM_DISPLAY"`
}

type ClassroomQueryResponse struct {
	E int           `json:"e"`
	M string        `json:"m"`
	D ClassroomData `json:"d"`
}

type ClassroomData struct {
	List map[string][]ClassroomInfo `json:"list"`
}

type ClassroomInfo struct {
	ID      string `json:"id"`
	FloorID string `json:"floorid"`
	Name    string `json:"name"`
	Kxsds   string `json:"kxsds"`
}

type SigninClassDto struct {
	CourseID       string `json:"courseId"`
	CourseName     string `json:"courseName"`
	ClassBeginTime string `json:"classBeginTime"`
	ClassEndTime   string `json:"classEndTime"`
	SignStatus     int    `json:"signStatus"`
}

type SigninStatusResponse struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    []SigninClassDto `json:"data"`
}

type SigninActionResponse struct {
	Code    int    `json:"code"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type BykcCourseDto struct {
	ID                    int64           `json:"id"`
	CourseName            string          `json:"courseName"`
	CoursePosition        *string         `json:"coursePosition"`
	CourseTeacher         *string         `json:"courseTeacher"`
	CourseStartDate       *string         `json:"courseStartDate"`
	CourseEndDate         *string         `json:"courseEndDate"`
	CourseSelectStartDate *string         `json:"courseSelectStartDate"`
	CourseSelectEndDate   *string         `json:"courseSelectEndDate"`
	CourseCancelEndDate   *string         `json:"courseCancelEndDate"`
	CourseMaxCount        int             `json:"courseMaxCount"`
	CourseCurrentCount    *int            `json:"courseCurrentCount"`
	Category              *string         `json:"category"`
	SubCategory           *string         `json:"subCategory"`
	AudienceCampuses      []string        `json:"audienceCampuses"`
	HasSignPoints         bool            `json:"hasSignPoints"`
	Status                string          `json:"status"`
	Selected              bool            `json:"selected"`
	Extra                 json.RawMessage `json:"-"`
}

type BykcCourseDetailDto struct {
	BykcCourseDto
	CourseContact        *string            `json:"courseContact"`
	CourseContactMobile  *string            `json:"courseContactMobile"`
	OrganizerCollegeName *string            `json:"organizerCollegeName"`
	CourseDesc           *string            `json:"courseDesc"`
	AudienceColleges     []string           `json:"audienceColleges"`
	AudienceTerms        []string           `json:"audienceTerms"`
	AudienceGroups       []string           `json:"audienceGroups"`
	SignConfig           *BykcSignConfigDto `json:"signConfig"`
	Checkin              *int               `json:"checkin"`
	Pass                 *int               `json:"pass"`
	CanSign              bool               `json:"canSign"`
	CanSignOut           bool               `json:"canSignOut"`
}

type BykcChosenCourseDto struct {
	ID                     int64              `json:"id"`
	CourseID               int64              `json:"courseId"`
	CourseName             string             `json:"courseName"`
	CoursePosition         *string            `json:"coursePosition"`
	CourseTeacher          *string            `json:"courseTeacher"`
	CourseStartDate        *string            `json:"courseStartDate"`
	CourseEndDate          *string            `json:"courseEndDate"`
	SelectDate             *string            `json:"selectDate"`
	CourseCancelEndDate    *string            `json:"courseCancelEndDate"`
	Category               *string            `json:"category"`
	SubCategory            *string            `json:"subCategory"`
	Checkin                int                `json:"checkin"`
	Score                  *int               `json:"score"`
	Pass                   *int               `json:"pass"`
	CanSign                bool               `json:"canSign"`
	CanSignOut             bool               `json:"canSignOut"`
	SignConfig             *BykcSignConfigDto `json:"signConfig"`
	CourseSignType         *int               `json:"courseSignType"`
	Homework               *string            `json:"homework"`
	HomeworkAttachmentName *string            `json:"homeworkAttachmentName"`
	HomeworkAttachmentPath *string            `json:"homeworkAttachmentPath"`
	SignInfo               *string            `json:"signInfo"`
}

type BykcSignConfigDto struct {
	SignStartDate    *string            `json:"signStartDate"`
	SignEndDate      *string            `json:"signEndDate"`
	SignOutStartDate *string            `json:"signOutStartDate"`
	SignOutEndDate   *string            `json:"signOutEndDate"`
	SignPoints       []BykcSignPointDto `json:"signPoints"`
}

type BykcSignPointDto struct {
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Radius float64 `json:"radius"`
}

type BykcUserProfileDto struct {
	ID          int64   `json:"id"`
	EmployeeID  string  `json:"employeeId"`
	RealName    string  `json:"realName"`
	StudentNo   *string `json:"studentNo"`
	StudentType *string `json:"studentType"`
	ClassCode   *string `json:"classCode"`
	CollegeName *string `json:"collegeName"`
	TermName    *string `json:"termName"`
}

type BykcStatisticsDto struct {
	TotalValidCount int                         `json:"totalValidCount"`
	Categories      []BykcCategoryStatisticsDto `json:"categories"`
}

type BykcCategoryStatisticsDto struct {
	CategoryName    string `json:"categoryName"`
	SubCategoryName string `json:"subCategoryName"`
	RequiredCount   int    `json:"requiredCount"`
	PassedCount     int    `json:"passedCount"`
	IsQualified     bool   `json:"isQualified"`
}

type BykcSignRequest struct {
	CourseID int64    `json:"courseId"`
	Lat      *float64 `json:"lat"`
	Lng      *float64 `json:"lng"`
	SignType int      `json:"signType"`
}

type BykcCoursesResponse struct {
	Courses     []BykcCourseDto `json:"courses"`
	Total       int             `json:"total"`
	TotalPages  int             `json:"totalPages"`
	CurrentPage int             `json:"currentPage"`
	PageSize    int             `json:"pageSize"`
}

type BykcSuccessResponse struct {
	Message string `json:"message"`
}

type CgyyVenueSiteDto struct {
	ID                    int     `json:"id"`
	SiteName              string  `json:"siteName"`
	VenueName             string  `json:"venueName"`
	CampusName            string  `json:"campusName"`
	SeatCount             *int    `json:"seatCount"`
	ReservationSpaceCount *int    `json:"reservationSpaceCount"`
	SiteTelephone         *string `json:"siteTelephone"`
	OpenStartDate         *string `json:"openStartDate"`
	OpenEndDate           *string `json:"openEndDate"`
}

type CgyyPurposeTypeDto struct {
	Key  int    `json:"key"`
	Name string `json:"name"`
}

type CgyyTimeSlotDto struct {
	ID        int    `json:"id"`
	BeginTime string `json:"beginTime"`
	EndTime   string `json:"endTime"`
	Label     string `json:"label"`
}

type CgyySlotStatusDto struct {
	TimeID            int     `json:"timeId"`
	ReservationStatus int     `json:"reservationStatus"`
	IsReservable      bool    `json:"isReservable"`
	StartDate         *string `json:"startDate"`
	EndDate           *string `json:"endDate"`
	TradeNo           *string `json:"tradeNo"`
	OrderID           *int    `json:"orderId"`
	UseNum            *int    `json:"useNum"`
	AlreadyNum        *int    `json:"alreadyNum"`
	TakeUp            *bool   `json:"takeUp"`
	TakeUpExplain     *string `json:"takeUpExplain"`
}

type CgyySpaceAvailabilityDto struct {
	SpaceID           int                 `json:"spaceId"`
	SpaceName         string              `json:"spaceName"`
	VenueSiteID       int                 `json:"venueSiteId"`
	VenueSpaceGroupID *int                `json:"venueSpaceGroupId"`
	Slots             []CgyySlotStatusDto `json:"slots"`
}

type CgyyDayInfoResponse struct {
	VenueSiteID         int                        `json:"venueSiteId"`
	ReservationDate     string                     `json:"reservationDate"`
	AvailableDates      []string                   `json:"availableDates"`
	TimeSlots           []CgyyTimeSlotDto          `json:"timeSlots"`
	Spaces              []CgyySpaceAvailabilityDto `json:"spaces"`
	ReservationToken    *string                    `json:"reservationToken"`
	ReservationTotalNum *int                       `json:"reservationTotalNum"`
}

type CgyyReservationSelectionDto struct {
	SpaceID           int  `json:"spaceId"`
	TimeID            int  `json:"timeId"`
	VenueSpaceGroupID *int `json:"venueSpaceGroupId"`
}

type CgyyReservationSubmitRequest struct {
	VenueSiteID                int                           `json:"venueSiteId"`
	ReservationDate            string                        `json:"reservationDate"`
	Selections                 []CgyyReservationSelectionDto `json:"selections"`
	Phone                      string                        `json:"phone"`
	Theme                      string                        `json:"theme"`
	PurposeType                int                           `json:"purposeType"`
	JoinerNum                  int                           `json:"joinerNum"`
	ActivityContent            string                        `json:"activityContent"`
	Joiners                    string                        `json:"joiners"`
	IsPhilosophySocialSciences bool                          `json:"isPhilosophySocialSciences"`
	IsOffSchoolJoiner          bool                          `json:"isOffSchoolJoiner"`
}

type CgyyOrderDto struct {
	ID                    int     `json:"id"`
	TradeNo               *string `json:"tradeNo"`
	VenueSiteID           *int    `json:"venueSiteId"`
	ReservationDate       *string `json:"reservationDate"`
	ReservationDateDetail *string `json:"reservationDateDetail"`
	VenueSpaceName        *string `json:"venueSpaceName"`
	CampusName            *string `json:"campusName"`
	VenueName             *string `json:"venueName"`
	SiteName              *string `json:"siteName"`
	ReservationStartDate  *string `json:"reservationStartDate"`
	ReservationEndDate    *string `json:"reservationEndDate"`
	Phone                 *string `json:"phone"`
	OrderStatus           *int    `json:"orderStatus"`
	PayStatus             *int    `json:"payStatus"`
	CheckStatus           *int    `json:"checkStatus"`
	Theme                 *string `json:"theme"`
	PurposeType           *int    `json:"purposeType"`
	PurposeTypeName       *string `json:"purposeTypeName"`
	JoinerNum             *int    `json:"joinerNum"`
	ActivityContent       *string `json:"activityContent"`
	Joiners               *string `json:"joiners"`
	CheckContent          *string `json:"checkContent"`
	HandleReason          *string `json:"handleReason"`
	Remark                *string `json:"remark"`
}

type CgyyOrdersPageResponse struct {
	Content       []CgyyOrderDto `json:"content"`
	TotalElements int            `json:"totalElements"`
	TotalPages    int            `json:"totalPages"`
	Size          int            `json:"size"`
	Number        int            `json:"number"`
}

type CgyyReservationSubmitResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Order   *CgyyOrderDto `json:"order"`
}

type CgyyLockCodeResponse struct {
	RawData any `json:"rawData"`
}

type SpocAssignmentsResponse struct {
	TermCode    string                     `json:"termCode"`
	TermName    *string                    `json:"termName"`
	Assignments []SpocAssignmentSummaryDto `json:"assignments"`
}

type SpocAssignmentSummaryDto struct {
	AssignmentID         string  `json:"assignmentId"`
	CourseID             string  `json:"courseId"`
	CourseName           string  `json:"courseName"`
	TeacherName          *string `json:"teacherName"`
	Title                string  `json:"title"`
	StartTime            *string `json:"startTime"`
	DueTime              *string `json:"dueTime"`
	Score                *string `json:"score"`
	SubmissionStatus     string  `json:"submissionStatus"`
	SubmissionStatusText string  `json:"submissionStatusText"`
}

type SpocAssignmentDetailDto struct {
	SpocAssignmentSummaryDto
	ContentPlainText *string `json:"contentPlainText"`
	ContentHTML      *string `json:"contentHtml"`
	SubmittedAt      *string `json:"submittedAt"`
}

type JudgeAssignmentsResponse struct {
	Assignments               []JudgeAssignmentSummaryDto `json:"assignments"`
	HistoricalCutoffCourseIDs []string                    `json:"historicalCutoffCourseIds"`
}

type JudgeAssignmentDetailKeyDto struct {
	CourseID     string `json:"courseId"`
	AssignmentID string `json:"assignmentId"`
}

type JudgeAssignmentDetailsRequest struct {
	Keys []JudgeAssignmentDetailKeyDto `json:"keys"`
}

type JudgeAssignmentDetailsResponse struct {
	Details []JudgeAssignmentDetailDto `json:"details"`
}

type JudgeAssignmentSummaryDto struct {
	CourseID             string  `json:"courseId"`
	CourseName           string  `json:"courseName"`
	AssignmentID         string  `json:"assignmentId"`
	Title                string  `json:"title"`
	StartTime            *string `json:"startTime"`
	DueTime              *string `json:"dueTime"`
	MaxScore             *string `json:"maxScore"`
	MyScore              *string `json:"myScore"`
	TotalProblems        int     `json:"totalProblems"`
	SubmittedCount       int     `json:"submittedCount"`
	SubmissionStatus     string  `json:"submissionStatus"`
	SubmissionStatusText string  `json:"submissionStatusText"`
}

type JudgeProblemDto struct {
	Name       string  `json:"name"`
	Score      *string `json:"score"`
	MaxScore   *string `json:"maxScore"`
	Status     string  `json:"status"`
	StatusText string  `json:"statusText"`
}

type JudgeAssignmentDetailDto struct {
	JudgeAssignmentSummaryDto
	Problems         []JudgeProblemDto `json:"problems"`
	ContentPlainText *string           `json:"contentPlainText"`
}

type LibBookLibraryDto struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	FreeNum  int                `json:"freeNum"`
	TotalNum int                `json:"totalNum"`
	Storeys  []LibBookStoreyDto `json:"storeys"`
}

type LibBookStoreyDto struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	FreeNum  int    `json:"freeNum"`
	TotalNum int    `json:"totalNum"`
}

type LibBookAreaDto struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AreaName   string `json:"areaName"`
	PremisesID string `json:"premisesId"`
	StoreyID   string `json:"storeyId"`
	FreeNum    int    `json:"freeNum"`
	TotalNum   int    `json:"totalNum"`
}

type LibBookTimeSlotDto struct {
	ID    string `json:"id"`
	Start string `json:"start"`
	End   string `json:"end"`
	Label string `json:"label"`
}

type LibBookAreaDetailDto struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	AvailableDates []string             `json:"availableDates"`
	TimeSlots      []LibBookTimeSlotDto `json:"timeSlots"`
}

type LibBookSeatDto struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	No          string `json:"no"`
	Status      string `json:"status"`
	StatusName  string `json:"statusName"`
	IsAvailable bool   `json:"isAvailable"`
}

type LibBookReserveRequest struct {
	AreaID    string `json:"areaId"`
	SeatID    string `json:"seatId"`
	Day       string `json:"day"`
	Segment   string `json:"segment"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

type LibBookReserveResponse struct {
	Success bool               `json:"success"`
	Message string             `json:"message"`
	Booking *LibBookBookingDto `json:"booking"`
}

type LibBookCancelResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type LibBookBookingsResponse struct {
	Bookings []LibBookBookingDto `json:"bookings"`
	Page     int                 `json:"page"`
	Limit    int                 `json:"limit"`
	Total    int                 `json:"total"`
}

type LibBookBookingDto struct {
	ID         string `json:"id"`
	NameMerge  string `json:"nameMerge"`
	AreaName   string `json:"areaName"`
	SeatNo     string `json:"seatNo"`
	Day        string `json:"day"`
	BeginTime  string `json:"beginTime"`
	EndTime    string `json:"endTime"`
	Status     string `json:"status"`
	StatusName string `json:"statusName"`
}

type YgdkTermSummaryDto struct {
	TermID      *int    `json:"termId"`
	TermName    *string `json:"termName"`
	TermCount   int     `json:"termCount"`
	TermTarget  *int    `json:"termTarget"`
	WeekCount   *int    `json:"weekCount"`
	WeekTarget  *int    `json:"weekTarget"`
	MonthCount  *int    `json:"monthCount"`
	MonthTarget *int    `json:"monthTarget"`
	DayCount    *int    `json:"dayCount"`
	GoodCount   *int    `json:"goodCount"`
}

type YgdkItemDto struct {
	ItemID int    `json:"itemId"`
	Name   string `json:"name"`
	Type   *int   `json:"type"`
	Sort   *int   `json:"sort"`
}

type YgdkOverviewResponse struct {
	Summary         YgdkTermSummaryDto `json:"summary"`
	ClassifyID      int                `json:"classifyId"`
	ClassifyName    string             `json:"classifyName"`
	DefaultItemID   *int               `json:"defaultItemId"`
	DefaultItemName *string            `json:"defaultItemName"`
	Items           []YgdkItemDto      `json:"items"`
}

type YgdkRecordDto struct {
	RecordID       int      `json:"recordId"`
	ItemID         *int     `json:"itemId"`
	ItemName       *string  `json:"itemName"`
	StartTime      *string  `json:"startTime"`
	EndTime        *string  `json:"endTime"`
	Place          *string  `json:"place"`
	Images         []string `json:"images"`
	IsOpen         bool     `json:"isOpen"`
	State          *int     `json:"state"`
	CreatedAt      *string  `json:"createdAt"`
	CreatedAtLabel *string  `json:"createdAtLabel"`
}

type YgdkRecordsPageResponse struct {
	Content []YgdkRecordDto `json:"content"`
	Total   int             `json:"total"`
	Page    int             `json:"page"`
	Size    int             `json:"size"`
	HasMore bool            `json:"hasMore"`
}

type YgdkClockinSubmitResponse struct {
	Success  bool                `json:"success"`
	Message  string              `json:"message"`
	RecordID *int                `json:"recordId"`
	Summary  *YgdkTermSummaryDto `json:"summary"`
}

type EvaluationCourse struct {
	ID          string  `json:"id"`
	Kcmc        string  `json:"kcmc"`
	Bpmc        string  `json:"bpmc"`
	IsEvaluated bool    `json:"isEvaluated"`
	Rwid        string  `json:"rwid"`
	Wjid        string  `json:"wjid"`
	Kcdm        string  `json:"kcdm"`
	Bpdm        *string `json:"bpdm"`
	Pjrdm       *string `json:"pjrdm"`
	Pjrmc       *string `json:"pjrmc"`
	Xnxq        *string `json:"xnxq"`
	Msid        string  `json:"msid"`
	Zdmc        *string `json:"zdmc"`
	Ypjcs       *int    `json:"ypjcs"`
	Xypjcs      *int    `json:"xypjcs"`
	Sxz         *string `json:"sxz"`
	Rwh         *string `json:"rwh"`
	Xn          *string `json:"xn"`
	Xq          *string `json:"xq"`
	Pjlxid      *string `json:"pjlxid"`
	Sfksqbpj    *string `json:"sfksqbpj"`
	Yxsfktjst   *string `json:"yxsfktjst"`
}

type EvaluationProgress struct {
	TotalCourses     int  `json:"totalCourses"`
	EvaluatedCourses int  `json:"evaluatedCourses"`
	PendingCourses   int  `json:"pendingCourses"`
	ProgressPercent  int  `json:"progressPercent"`
	IsCompleted      bool `json:"isCompleted"`
}

type EvaluationCoursesResponse struct {
	Courses  []EvaluationCourse `json:"courses"`
	Progress EvaluationProgress `json:"progress"`
}

type EvaluationResult struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	CourseName string `json:"courseName"`
}
