package feemanager

import (
	"path/filepath"
	"time"

	"go.sia.tech/siad/build"
	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/modules/consensus"
	"go.sia.tech/siad/modules/gateway"
	"go.sia.tech/siad/modules/transactionpool"
	"go.sia.tech/siad/modules/wallet"
	"go.sia.tech/siad/types"
	"gitlab.com/NebulousLabs/fastrand"
)

// addRandomFee will add a random fee to the FeeManager
func addRandomFee(fm *FeeManager) (modules.FeeUID, error) {
	fee := randomFee()
	uid, err := fm.AddFee(fee.Address, fee.Amount, fee.AppUID, fee.Recurring)
	if err != nil {
		return "", err
	}
	return uid, nil
}

// addRandomFees will add a random number of fees to the FeeManager, always at
// least 1.
func addRandomFees(fm *FeeManager) ([]modules.FeeUID, error) {
	return addRandomFeesN(fm, fastrand.Intn(5)+1)
}

// addRandomFeesN will add N number of fees to the FeeManager
func addRandomFeesN(fm *FeeManager, n int) ([]modules.FeeUID, error) {
	var uids []modules.FeeUID
	for i := 0; i < n; i++ {
		uid, err := addRandomFee(fm)
		if err != nil {
			return nil, err
		}
		uids = append(uids, uid)
	}
	return uids, nil
}

// randomFee creates and returns a fee with random values
func randomFee() modules.AppFee {
	randBytes := fastrand.Bytes(16)
	var uh types.UnlockHash
	copy(uh[:], randBytes)
	return modules.AppFee{
		Address:            uh,
		Amount:             types.NewCurrency64(fastrand.Uint64n(1e9)),
		AppUID:             modules.AppUID(uniqueID()),
		PaymentCompleted:   fastrand.Intn(2) == 0,
		PayoutHeight:       types.BlockHeight(fastrand.Uint64n(1e9)),
		Recurring:          fastrand.Intn(2) == 0,
		Timestamp:          time.Now().Unix(),
		TransactionCreated: fastrand.Intn(2) == 0,
		FeeUID:             uniqueID(),
	}
}

// newTestingFeeManager creates a FeeManager for testing
func newTestingFeeManager(testName string) (*FeeManager, error) {
	// Create testdir
	testDir := build.TempDir("feemanager", testName)

	// Create Dependencies
	cs, tp, w, err := testingDependencies(testDir)
	if err != nil {
		return nil, err
	}

	// Return FeeManager
	return NewCustomFeeManager(cs, tp, w, filepath.Join(testDir, modules.FeeManagerDir), modules.ProdDependencies)
}

// testingDependencies creates the dependencies needed for the FeeManager
func testingDependencies(testdir string) (modules.ConsensusSet, modules.TransactionPool, modules.Wallet, error) {
	// Create a gateway
	g, err := gateway.New("localhost:0", false, filepath.Join(testdir, modules.GatewayDir))
	if err != nil {
		return nil, nil, nil, err
	}
	// Create a consensus set
	cs, errChan := consensus.New(g, false, filepath.Join(testdir, modules.ConsensusDir))
	if err := <-errChan; err != nil {
		return nil, nil, nil, err
	}
	// Create a tpool
	tp, err := transactionpool.New(cs, g, filepath.Join(testdir, modules.TransactionPoolDir))
	if err != nil {
		return nil, nil, nil, err
	}
	// Create a wallet and unlock it
	w, err := wallet.New(cs, tp, filepath.Join(testdir, modules.WalletDir))
	if err != nil {
		return nil, nil, nil, err
	}
	key := crypto.GenerateSiaKey(crypto.TypeDefaultWallet)
	_, err = w.Encrypt(key)
	if err != nil {
		return nil, nil, nil, err
	}
	err = w.Unlock(key)
	if err != nil {
		return nil, nil, nil, err
	}

	return cs, tp, w, nil
}
