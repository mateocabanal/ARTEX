package server

import (
	"encoding/json"
	"log"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/egress"
)

// startupTargetIdentity loads the operator-visible target request signature.
// Invalid persisted data is ignored rather than allowed to corrupt requests.
func startupTargetIdentity(pg *db.DB) egress.TargetIdentity {
	var identity egress.TargetIdentity
	value, ok, err := pg.GetSetting(settingTargetIdentity)
	if err != nil {
		log.Printf("[egress] read target identity: %v; signature disabled", err)
		return egress.TargetIdentity{}
	}
	if !ok {
		return egress.TargetIdentity{}
	}
	if err := json.Unmarshal([]byte(value), &identity); err != nil {
		log.Printf("[egress] invalid saved target identity JSON: %v; signature disabled", err)
		return egress.TargetIdentity{}
	}
	valid, err := egress.ValidateTargetIdentity(identity)
	if err != nil {
		log.Printf("[egress] invalid saved target identity: %v; signature disabled", err)
		return egress.TargetIdentity{}
	}
	return valid
}
