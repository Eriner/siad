package miner

import "go.sia.tech/siad/modules"

// Alerts implements the modules.Alerter interface for the miner.
func (m *Miner) Alerts() (crit, err, warn []modules.Alert) {
	return []modules.Alert{}, []modules.Alert{}, []modules.Alert{}
}
