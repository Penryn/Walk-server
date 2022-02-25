package model

type Person struct {
	OpenId    string `gorm:"primaryKey"` // openID
	Name      string
	Gender    int8
	StuId     string
	Campus    uint8
	Identity  string // 身份证号
	Status    uint8  // 0 没加入团队，1 加入了并且是队员，2 是队长
	Qq        string
	Wechat    string
	College   string // 学院
	Tel       string
	CreatedOp uint8
	JoinOp    uint8
	TeamId    int `gorm:"index;default:-1"`
}
