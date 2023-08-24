package migrate

import (
	"encoding/json"

	"github.com/gravitl/netmaker/database"
	"github.com/gravitl/netmaker/logger"
	"github.com/gravitl/netmaker/logic"
	"github.com/gravitl/netmaker/models"
	"golang.org/x/exp/slog"
)

// Run - runs all migrations
func Run() {
	updateEnrollmentKeys()
	assignSuperAdmin()
}

func assignSuperAdmin() {
	ok, _ := logic.HasSuperAdmin()
	if !ok {
		createdSuperAdmin := false
		users, err := logic.GetUsers()
		if err == nil {
			for _, u := range users {
				if u.IsAdmin {
					user, err := logic.GetUser(u.UserName)
					if err != nil {
						slog.Error("error getting user", "user", u.UserName, "error", err.Error())
						continue
					}
					user.IsSuperAdmin = true
					user.IsAdmin = false
					err = logic.UpsertUser(*user)
					if err != nil {
						slog.Error("error updating user to superadmin", "user", user.UserName, "error", err.Error())
						continue
					} else {
						createdSuperAdmin = true
					}
					break
				}
			}
		}
		if !createdSuperAdmin {
			logger.FatalLog0("failed to create superadmin!!")
		}
	}
}

func updateEnrollmentKeys() {
	rows, err := database.FetchRecords(database.ENROLLMENT_KEYS_TABLE_NAME)
	if err != nil {
		return
	}
	for _, row := range rows {
		var key models.EnrollmentKey
		if err = json.Unmarshal([]byte(row), &key); err != nil {
			continue
		}
		if key.Type != models.Undefined {
			logger.Log(2, "migration: enrollment key type already set")
			continue
		} else {
			logger.Log(2, "migration: updating enrollment key type")
			if key.Unlimited {
				key.Type = models.Unlimited
			} else if key.UsesRemaining > 0 {
				key.Type = models.Uses
			} else if !key.Expiration.IsZero() {
				key.Type = models.TimeExpiration
			}
		}
		data, err := json.Marshal(key)
		if err != nil {
			logger.Log(0, "migration: marshalling enrollment key: "+err.Error())
			continue
		}
		if err = database.Insert(key.Value, string(data), database.ENROLLMENT_KEYS_TABLE_NAME); err != nil {
			logger.Log(0, "migration: inserting enrollment key: "+err.Error())
			continue
		}

	}
}
