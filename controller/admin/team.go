package admin

import (
	"errors"
	"strconv"
	"time"
	"walk-server/constant"
	"walk-server/global"
	"walk-server/middleware"
	"walk-server/model"
	"walk-server/service/adminService"
	"walk-server/service/teamService"
	"walk-server/service/userService"
	"walk-server/utility"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TeamForm struct {
	CodeType uint   `form:"code_type" binding:"required,oneof=1 2"` // 1团队码2签到码
	Content  string `form:"content" binding:"required"`             // 团队码为team_id，签到码为code
}

func GetTeam(c *gin.Context) {
	var postForm TeamForm
	err := c.ShouldBindQuery(&postForm)
	if err != nil {
		utility.ResponseError(c, "参数错误")
		return
	}
	user, _ := adminService.GetAdminByJWT(c)
	var team *model.Team
	if postForm.CodeType == 1 {
		teamID, convErr := strconv.ParseUint(postForm.Content, 10, 32)
		if convErr != nil {
			utility.ResponseError(c, "参数错误")
			return
		}
		team, err = teamService.GetTeamByID(uint(teamID))
	} else {
		team, err = teamService.GetTeamByCode(postForm.Content)
	}
	if team == nil || err != nil {
		utility.ResponseError(c, "二维码错误，队伍查找失败")
		return
	}

	b := middleware.CheckRoute(user, team)
	if !b {
		utility.ResponseError(c, "该队伍为其他路线")
		return
	}

	var persons []model.Person
	global.DB.Where("team_id = ?", team.ID).Find(&persons)

	var memberData []gin.H
	for _, member := range persons {
		memberData = append(memberData, gin.H{
			"name":    member.Name,
			"gender":  member.Gender,
			"open_id": member.OpenId,
			"campus":  member.Campus,
			"type":    member.Type,
			"contact": gin.H{
				"qq":     member.Qq,
				"wechat": member.Wechat,
				"tel":    member.Tel,
			},
			"walk_status": member.WalkStatus,
		})
	}
	point := constant.GetPointName(team.Route, team.Point)
	utility.ResponseSuccess(c, gin.H{
		"team": gin.H{
			"id":          team.ID,
			"name":        team.Name,
			"route":       team.Route,
			"password":    team.Password,
			"allow_match": team.AllowMatch,
			"slogan":      team.Slogan,
			"point":       point,
			"status":      team.Status,
			"start_num":   team.StartNum,
			"code":        team.Code,
		},
		"admin":  user,
		"member": memberData,
	})
}

type BindTeamForm struct {
	TeamID uint   `json:"team_id" binding:"required"`
	Type   uint   `json:"type" binding:"required,eq=2"`
	Code   string `json:"code" binding:"required"`
}

func BindTeam(c *gin.Context) {
	var postForm BindTeamForm
	err := c.ShouldBindJSON(&postForm)

	if err != nil {
		utility.ResponseError(c, "参数错误")
		return
	}

	user, _ := adminService.GetAdminByJWT(c)
	team, err := teamService.GetTeamByID(postForm.TeamID)
	if team == nil || err != nil {
		utility.ResponseError(c, "队伍查找失败，请重新核对")
		return
	}

	b := middleware.CheckRoute(user, team)
	if !b {
		utility.ResponseError(c, "该队伍为其他路线")
		return
	}

	_, err = teamService.GetTeamByCode(postForm.Code)
	if err == nil {
		utility.ResponseError(c, "二维码已绑定")
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		utility.ResponseError(c, "服务错误，请联系负责人")
		return
	}
	var persons []model.Person
	global.DB.Where("team_id = ?", team.ID).Find(&persons)
	flag := true
	num := uint(0)
	for _, p := range persons {
		if p.WalkStatus != 3 && p.WalkStatus != 4 {
			flag = false
			break
		} else {
			if p.WalkStatus == 3 {
				num++
			}
		}
	}

	if !flag {
		utility.ResponseError(c, "还有成员未确认状态")
		return
	}

	if (team.Num+1)/2 > uint8(num) {
		utility.ResponseError(c, "团队人数不足，无法绑定")
		return
	}

	team.Code = postForm.Code
	team.Point = 0
	team.Status = 5
	team.StartNum = num
	team.Time = time.Now()
	teamService.Update(*team)
	utility.ResponseSuccess(c, nil)
}

type TeamStatusForm struct {
	CodeType uint   `json:"code_type" binding:"required"` //1团队码2签到码
	Content  string `json:"content" binding:"required"`   //团队码为team_id，签到码为code
}

func UpdateTeamStatus(c *gin.Context) {
	var postForm TeamStatusForm
	err := c.ShouldBindJSON(&postForm)

	if err != nil {
		utility.ResponseError(c, "参数错误")
		return
	}

	user, _ := adminService.GetAdminByJWT(c)
	var team *model.Team
	if postForm.CodeType == 1 {
		teamID, convErr := strconv.ParseUint(postForm.Content, 10, 32)
		if convErr != nil {
			utility.ResponseError(c, "参数错误")
			return
		}
		team, err = teamService.GetTeamByID(uint(teamID))
	} else if postForm.CodeType == 2 {
		team, err = teamService.GetTeamByCode(postForm.Content)
	} else {
		utility.ResponseError(c, "参数错误")
		return
	}

	if team == nil || err != nil {
		utility.ResponseError(c, "队伍查找失败，请重新核对")
		return
	}

	b := middleware.CheckRoute(user, team)
	if !b {
		utility.ResponseError(c, "该队伍为其他路线")
		return
	}
	if team.Status == 1 {
		utility.ResponseError(c, "团队起点未扫码")
		return
	} else if team.Status == 3 || team.Status == 4 {
		utility.ResponseError(c, "团队已结束，有疑问请咨询管理员")
		return
	}
	var persons []model.Person
	global.DB.Where("team_id = ?", team.ID).Find(&persons)
	num := uint(0)
	for _, p := range persons {
		if p.WalkStatus == 3 || p.WalkStatus == 2 {
			num++
		}
	}

	if num == 0 {
		team.Status = 3
		team.Point = int8(constant.PointMap[team.Route])
		teamService.Update(*team)
		utility.ResponseSuccess(c, gin.H{
			"progress_num": 0,
		})
		return
	}

	// 各路线点位签到逻辑设置
	switch team.Route {
	case 2:
		if user.Route == 3 && (user.Point == 2 || user.Point == 3 || user.Point == 4) {
			utility.ResponseError(c, "该队伍为半程路线，让队伍继续往前走就行")
			return
		}
		if user.Point > 2 {
			team.Point = user.Point - 2
		} else {
			team.Point = user.Point
		}
	case 3:
		if user.Route == 2 && user.Point == 2 {
			utility.ResponseError(c, "该队伍为全程路线，让队伍继续往前走就行")
			return
		}
		team.Point = user.Point
	default:
		team.Point = user.Point
	}

	for _, p := range persons {
		if p.WalkStatus == 3 {
			p.WalkStatus = 2
			userService.Update(p)
		}
	}
	team.Time = time.Now()
	team.Status = 2
	teamService.Update(*team)
	utility.ResponseSuccess(c, gin.H{
		"progress_num": num,
	})
}

type PostDestinationForm struct {
	TeamID uint `json:"team_id" binding:"required"`
	Status uint `json:"status" binding:"required,oneof=1 2"`
}

func PostDestination(c *gin.Context) {
	var postForm PostDestinationForm
	err := c.ShouldBindJSON(&postForm)

	if err != nil {
		utility.ResponseError(c, "参数错误")
		return
	}

	user, _ := adminService.GetAdminByJWT(c)
	team, err := teamService.GetTeamByID(postForm.TeamID)
	if team == nil || err != nil {
		utility.ResponseError(c, "队伍查找失败，请重新核对")
		return
	}

	b := middleware.CheckRoute(user, team)
	if !b {
		utility.ResponseError(c, "该队伍为其他路线")
		return
	}

	var persons []model.Person
	global.DB.Where("team_id = ?", team.ID).Find(&persons)
	num := uint(0)
	for _, p := range persons {
		if p.WalkStatus == 3 || p.WalkStatus == 2 {
			num++
		}
	}

	if team.Status == 4 || team.Status == 3 {
		utility.ResponseError(c, "队伍状态已确认，有疑问请咨询管理员")
		return
	}

	if num == 0 {
		team.Status = 3
		team.Point = int8(constant.PointMap[team.Route])
		teamService.Update(*team)
		utility.ResponseSuccess(c, nil)
		return
	}

	team.Point = int8(constant.PointMap[team.Route])
	team.Time = time.Now()

	if postForm.Status == 1 {
		for _, p := range persons {
			if p.WalkStatus == 2 || p.WalkStatus == 3 {
				p.WalkStatus = 5
				userService.Update(p)
			}
		}
		team.Status = 4
		teamService.Update(*team)
		utility.ResponseSuccess(c, nil)
		return
	} else {
		team.Status = 3
		teamService.Update(*team)
		utility.ResponseSuccess(c, nil)
		return
	}
}

type RegroupForm struct {
	Jwts   []string `json:"jwts" binding:"required"`
	Secret string   `json:"secret" binding:"required"`
	Route  uint8    `json:"route" binding:"required"`
}

func Regroup(c *gin.Context) {
	var postForm RegroupForm
	err := c.ShouldBindJSON(&postForm)

	if err != nil {
		utility.ResponseError(c, "参数错误")
		return
	}
	if postForm.Secret != global.Config.GetString("server.secret") {
		utility.ResponseError(c, "密码错误")
		return
	}

	var persons []model.Person
	processedJwts := make(map[string]bool)
	for _, jwt := range postForm.Jwts {
		if processedJwts[jwt] {
			utility.ResponseError(c, "重复扫码,请重新提交")
		}
		processedJwts[jwt] = true

		jwtToken := jwt[7:]
		jwtData, err := utility.ParseToken(jwtToken)

		if err != nil {
			utility.ResponseError(c, "扫码错误，请重新扫码")
			return
		}

		// 获取个人信息
		person, err := model.GetPerson(jwtData.OpenID)

		if err != nil {
			utility.ResponseError(c, "扫码错误，请重新扫码")
			return
		}

		// 如果已有队伍则获取队伍信息
		if person.TeamId != -1 {
			team, _ := teamService.GetTeamByID(uint(person.TeamId))
			if team.Status != 1 {
				utility.ResponseError(c, person.Name+"的原队伍已开始，请勿重新组队")
				return
			}
		}

		persons = append(persons, *person)
	}

	for _, person := range persons {
		// 如果已有队伍则退出
		if person.TeamId != -1 {
			captain, persons := model.GetPersonsInTeam(person.TeamId)
			for _, p := range persons {
				p.TeamId = -1
				p.Status = 0
				p.WalkStatus = 1
				userService.Update(p)
			}
			captain.TeamId = -1
			captain.Status = 0
			captain.WalkStatus = 1
			userService.Update(captain)
			team, err := teamService.GetTeamByID(uint(person.TeamId))
			if err == nil {
				err = teamService.Delete(*team)
				if err != nil {
					utility.ResponseError(c, "服务错误")
					return
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				utility.ResponseError(c, "服务错误")
				return
			}
		}
	}

	// 创建新队伍，第一个人作为队长
	newTeam := model.Team{
		Name:       "新队伍",
		Route:      postForm.Route,
		Password:   "123456",
		AllowMatch: true,
		Slogan:     "新的开始",
		Point:      -1,
		Status:     1,
		StartNum:   uint(0),
		Num:        uint8(len(persons)),
		Captain:    persons[0].OpenId,
		Submit:     true,
		Time:       time.Now().Add(8 * time.Hour),
	}
	err = teamService.Create(newTeam)
	if err != nil {
		utility.ResponseError(c, "服务错误")
		return
	}

	team, err := teamService.GetTeamByCaptain(persons[0].OpenId)
	if err != nil {
		utility.ResponseError(c, "服务错误")
		return
	}

	// 更新每个人的队伍ID
	for i, person := range persons {
		person.TeamId = int(team.ID)
		if i == 0 {
			person.Status = 2
		} else {
			person.Status = 1
		}
		userService.Update(person)
	}
	global.Rdb.SAdd(global.Rctx, "teams", strconv.Itoa(int(newTeam.ID)))

	utility.ResponseSuccess(c, gin.H{
		"team_id": newTeam.ID,
	})
}

type SubmitTeamForm struct {
	TeamID uint   `json:"team_id" binding:"required"`
	Secret string `json:"secret" binding:"required"`
}

func SubmitTeam(c *gin.Context) {
	var postForm SubmitTeamForm
	err := c.ShouldBindJSON(&postForm)

	if err != nil {
		utility.ResponseError(c, "参数错误")
		return
	}
	if postForm.Secret != global.Config.GetString("server.secret") {
		utility.ResponseError(c, "密码错误")
		return
	}
	team, err := teamService.GetTeamByID(postForm.TeamID)
	if team == nil || err != nil {
		utility.ResponseError(c, "队伍查找失败，请重新核对")
		return
	}

	team.Submit = true
	teamService.Update(*team)
	global.Rdb.SAdd(global.Rctx, "teams", strconv.Itoa(int(team.ID)))
	utility.ResponseSuccess(c, nil)

}

type GetDetailForm struct {
	Secret string `form:"secret" binding:"required"`
}

type RouteDetail struct {
	Count int64  `json:"count"`
	Label string `json:"label"`
}

// GetDetail 获取全部路线的点位信息
func GetDetail(c *gin.Context) {
	var postForm GetDetailForm
	if err := c.ShouldBindQuery(&postForm); err != nil {
		utility.ResponseError(c, "参数错误")
		return
	}
	if postForm.Secret != global.Config.GetString("server.secret") {
		utility.ResponseError(c, "密码错误")
		return
	}

	routes := map[string]int{
		"zh":      1,
		"pfHalf":  2,
		"pfAll":   3,
		"mgsHalf": 4,
		"mgsAll":  5,
	}

	resultMap := make(map[string][]int64)
	for key, route := range routes {
		resultMap[key] = make([]int64, constant.PointMap[uint8(route)]+3)
	}

	// 获取各点位人数
	getPointCounts := func(route int, status []int, team_stuats []int, points []int64) {
		var pointCounts []struct {
			Point int64
			Count int64
		}
		global.DB.Model(&model.Person{}).
			Select("teams.point, count(*) as count").
			Joins("JOIN teams ON people.team_id = teams.id").
			Where("teams.route = ? AND people.walk_status IN ? AND teams.status IN ?", route, status, team_stuats).
			Group("teams.point").
			Order("teams.point").
			Scan(&pointCounts)

		for _, pointCount := range pointCounts {
			if pointCount.Point >= 0 && int(pointCount.Point) < int(constant.PointMap[uint8(route)])+1 {
				points[pointCount.Point+1] = pointCount.Count
			}
		}

	}

	// 获取各路线未开始人数
	getStartCounts := func(route int, points *int64) {
		global.DB.Model(&model.Person{}).
			Select("count(*) as count").
			Joins("JOIN teams ON people.team_id = teams.id").
			Where("teams.route = ? AND people.walk_status = 1 And teams.submit = 1", route).
			Pluck("count", points)
	}

	// 获取各路线已结束和下撤人数
	appendEndCounts := func(route int, points []int64) {
		var endCount5, endCount4 int64
		global.DB.Model(&model.Person{}).
			Select("count(*) as count").
			Joins("JOIN teams ON people.team_id = teams.id").
			Where("teams.route = ? AND people.walk_status = 5", route).
			Pluck("count", &endCount5)
		global.DB.Model(&model.Person{}).
			Select("count(*) as count").
			Joins("JOIN teams ON people.team_id = teams.id").
			Where("teams.route = ? AND people.walk_status = 4", route).
			Pluck("count", &endCount4)
		points[len(points)-2] = endCount5
		points[len(points)-1] = endCount4
	}

	// 状态：进行中、未开始、已结束
	personStatusInProgress := []int{2, 3}
	teamStatusInProgress := []int{2, 5}

	for key, route := range routes {
		getPointCounts(route, personStatusInProgress, teamStatusInProgress, resultMap[key])
		getStartCounts(route, &resultMap[key][0])
		appendEndCounts(route, resultMap[key])
	}

	processRoute := func(routeName string, routeID int) []RouteDetail {
		details := make([]RouteDetail, len(resultMap[routeName]))
		for i, count := range resultMap[routeName] {
			label := ""
			switch i {
			case 0:
				label = "未开始"
			case len(resultMap[routeName]) - 2:
				label = "已结束"
			case len(resultMap[routeName]) - 1:
				label = "下撤"
			default:
				label = constant.GetPointName(uint8(routeID), int8(i-1))
			}
			details[i] = RouteDetail{
				Count: count,
				Label: label,
			}
		}
		return details
	}

	// 处理各路线的数据
	zhDetails := processRoute("zh", 1)
	pfAllDetails := processRoute("pfAll", 3)
	pfHalfDetails := processRoute("pfHalf", 2)
	mgsHalfDetails := processRoute("mgsHalf", 4)
	mgsAllDetails := processRoute("mgsAll", 5)

	// 返回结果
	utility.ResponseSuccess(c, gin.H{
		"zh":      zhDetails,
		"pfAll":   pfAllDetails,
		"pfHalf":  pfHalfDetails,
		"mgsHalf": mgsHalfDetails,
		"mgsAll":  mgsAllDetails,
	})
}

type Result struct {
	Route    string
	TeamType string
	TeamNum  int64
	TotalNum int64
}

// 定义结果类型
type Results struct {
	Zh      []Result `json:"zh"`
	PfHalf  []Result `json:"pfHalf"`
	PfAll   []Result `json:"pfAll"`
	MgsHalf []Result `json:"mgsHalf"`
	MgsAll  []Result `json:"mgsAll"`
}

// GetSubmitDetail 获取已提交队伍信息
func GetSubmitDetail(c *gin.Context) {
	var postForm GetDetailForm
	if err := c.ShouldBindQuery(&postForm); err != nil {
		utility.ResponseError(c, "参数错误")
		return
	}
	if postForm.Secret != global.Config.GetString("server.secret") {
		utility.ResponseError(c, "密码错误")
		return
	}

	// 创建结果集合
	var results Results
	submit := 1

	// 定义路线和队伍类型的映射
	routes := []struct {
		Name  string
		Route int
	}{
		{"朝晖", 1},
		{"屏峰半程", 2},
		{"屏峰全程", 3},
		{"莫干山半程", 4},
		{"莫干山全程", 5},
	}

	teamTypes := []struct {
		Type    int
		Name    string
		IsMixed bool // 是否为师生队
	}{
		{1, "学生队", false},
		{2, "教师队", false},
		{2, "师生队", true},
		{3, "校友队", false},
	}

	// 获取各个路线的队伍数据
	for _, r := range routes {
		for _, t := range teamTypes {
			teamCount, totalCount := getTeamStats(r.Route, submit, t.Type, t.IsMixed)

			switch r.Name {
			case "朝晖":
				results.Zh = append(results.Zh, Result{
					Route:    r.Name,
					TeamType: t.Name,
					TeamNum:  teamCount,
					TotalNum: totalCount,
				})
			case "屏峰半程":
				results.PfHalf = append(results.PfHalf, Result{
					Route:    r.Name,
					TeamType: t.Name,
					TeamNum:  teamCount,
					TotalNum: totalCount,
				})
			case "屏峰全程":
				results.PfAll = append(results.PfAll, Result{
					Route:    r.Name,
					TeamType: t.Name,
					TeamNum:  teamCount,
					TotalNum: totalCount,
				})
			case "莫干山半程":
				results.MgsHalf = append(results.MgsHalf, Result{
					Route:    r.Name,
					TeamType: t.Name,
					TeamNum:  teamCount,
					TotalNum: totalCount,
				})
			case "莫干山全程":
				results.MgsAll = append(results.MgsAll, Result{
					Route:    r.Name,
					TeamType: t.Name,
					TeamNum:  teamCount,
					TotalNum: totalCount,
				})
			}
		}
	}

	// 返回结果
	utility.ResponseSuccess(c, gin.H{"results": results})
}

// 定义一个函数用于统计队伍数量和总人数
func getTeamStats(route int, submit int, captainType int, hasStudent bool) (int64, int64) {
	var teamCount int64
	var totalCount int64

	// 基础查询
	teamQuery := global.DB.Model(&model.Team{}).
		Joins("JOIN people AS captain ON captain.open_id = teams.captain").
		Where("teams.route = ? AND teams.submit = ?", route, submit).
		Where("captain.type = ?", captainType)

	// 根据是否有学生的条件进行筛选
	if captainType == 2 {
		if hasStudent {
			teamQuery = teamQuery.Where("EXISTS (SELECT 1 FROM people WHERE people.team_id = teams.id AND people.type = 1)")
		} else {
			teamQuery = teamQuery.Where("NOT EXISTS (SELECT 1 FROM people WHERE people.team_id = teams.id AND people.type = 1)")
		}
	}

	// 统计队伍数量
	teamQuery.Count(&teamCount)

	// 统计总人数
	totalQuery := global.DB.Model(&model.Person{}).
		Joins("JOIN teams ON people.team_id = teams.id").
		Joins("JOIN people AS captain ON captain.open_id = teams.captain").
		Where("teams.route = ? AND teams.submit = ?", route, submit).
		Where("captain.type = ?", captainType)

	if captainType == 2 {
		if hasStudent {
			totalQuery = totalQuery.Where("EXISTS (SELECT 1 FROM people WHERE people.team_id = teams.id AND people.type = 1)")
		} else {
			totalQuery = totalQuery.Where("NOT EXISTS (SELECT 1 FROM people WHERE people.team_id = teams.id AND people.type = 1)")
		}
	}

	totalQuery.Count(&totalCount)

	return teamCount, totalCount
}

type allTeamForm struct {
	Secret string `form:"secret" binding:"required"`
	TeamID uint   `form:"team_id" binding:"required"` // 团队码为team_id
}

func GetTeamBySecret(c *gin.Context) {
	var postForm allTeamForm
	err := c.ShouldBindQuery(&postForm)
	if err != nil {
		utility.ResponseError(c, "参数错误")
		return
	}
	if postForm.Secret != global.Config.GetString("server.secret") {
		utility.ResponseError(c, "密码错误")
		return
	}
	var team *model.Team
	team, err = teamService.GetTeamByID(postForm.TeamID)
	if team == nil || err != nil {
		utility.ResponseError(c, "队伍编号输入错误，队伍查找失败")
		return
	}

	var persons []model.Person
	global.DB.Where("team_id = ?", team.ID).Find(&persons)

	var memberData []gin.H
	for _, member := range persons {
		memberData = append(memberData, gin.H{
			"name":    member.Name,
			"gender":  member.Gender,
			"open_id": member.OpenId,
			"campus":  member.Campus,
			"status":  member.Status,
			"type":    member.Type,
			"contact": gin.H{
				"qq":     member.Qq,
				"wechat": member.Wechat,
				"tel":    member.Tel,
			},
			"walk_status": member.WalkStatus,
		})
	}
	point := constant.GetPointName(team.Route, team.Point)
	utility.ResponseSuccess(c, gin.H{
		"team": gin.H{
			"id":          team.ID,
			"name":        team.Name,
			"route":       team.Route,
			"password":    team.Password,
			"allow_match": team.AllowMatch,
			"slogan":      team.Slogan,
			"point":       point,
			"status":      team.Status,
			"start_num":   team.StartNum,
			"code":        team.Code,
			"submit":      team.Submit,
		},
		"member": memberData,
	})
}
