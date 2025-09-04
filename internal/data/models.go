package data

import "database/sql"

type Models struct {
	User UserModel
}

func NewModels(db *sql.DB) Models {
	return Models{
		User: UserModel{
			DB: db,
		},
	}
}
