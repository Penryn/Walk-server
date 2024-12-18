package admin

import (
	"errors"
	"walk-server/global"
	"walk-server/middleware"
	"walk-server/model"
	"walk-server/service/adminService"
	"walk-server/service/userService"
	"walk-server/utility"

	"github.com/gin-gonic/gin"
)

type UserStatusForm struct {
	UserID string `json:"user_id" binding:"required"`
	Status int    `json:"status" binding:"required,oneof=1 2"`
}

type UserStatusList struct {
	List []UserStatusForm `json:"list" binding:"required"`
}

// UserStatus handles user status updates
func UserStatus(c *gin.Context) {
	var postForm UserStatusList
	if err := c.ShouldBindJSON(&postForm); err != nil {
		utility.ResponseError(c, "参数错误")
		return
	}

	// 获取管理员信息
	user, _ := adminService.GetAdminByJWT(c)

	// 批量获取用户和队伍信息
	users, teams, err := getUsersAndTeams(postForm.List)
	if err != nil {
		utility.ResponseError(c, err.Error())
		return
	}

	// 验证用户权限
	for _, person := range users {
		team, exists := teams[person.TeamId]
		if !exists {
			utility.ResponseError(c, "队伍信息获取失败")
			return
		}

		// 管理员只能管理自己所在的校区
		if !middleware.CheckRoute(user, &team) {
			utility.ResponseError(c, "该队伍为其他路线")
			return
		}

		// 验证毅行状态
		if person.WalkStatus == 5 {
			utility.ResponseError(c, "成员已结束毅行")
			return
		}
	}

	// 更新用户状态
	for _, form := range postForm.List {
		person := users[form.UserID]
		if form.Status == 1 {
			person.WalkStatus = 3
		} else {
			person.WalkStatus = 4
		}
		userService.Update(*person)
	}

	utility.ResponseSuccess(c, nil)
}

// getUsersAndTeams retrieves user and team data for the given user IDs
func getUsersAndTeams(forms []UserStatusForm) (map[string]*model.Person, map[int]model.Team, error) {
	userMap := make(map[string]*model.Person)
	teamMap := make(map[int]model.Team)

	for _, form := range forms {
		person, err := model.GetPerson(form.UserID)
		if err != nil {
			return nil, nil, errors.New("扫码错误，查找用户失败，请再次核对")
		}
		userMap[form.UserID] = person

		if _, exists := teamMap[person.TeamId]; !exists {
			var team model.Team
			if err := global.DB.Where("id = ?", person.TeamId).Take(&team).Error; err != nil {
				return nil, nil, errors.New("队伍信息获取失败")
			}
			teamMap[person.TeamId] = team
		}
	}

	return userMap, teamMap, nil
}
