package service

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
)

var (
	ErrInsufficientBalance = errors.New("insufficient wallet balance")
	ErrIdempotencyConflict = errors.New("wallet idempotency key was reused with different mutation data")
	ErrWalletStateInvalid  = errors.New("wallet balance or frozen amount is invalid")
)

type WalletMutation struct {
	EntryNo       string
	AccountID     uuid.UUID
	Amount        int64
	Type          string
	ReferenceType string
	ReferenceID   *uuid.UUID
	Description   string
}

func ApplyWalletMutation(db *gorm.DB, mutation WalletMutation) (*model.WalletEntry, error) {
	if mutation.EntryNo == "" || mutation.AccountID == uuid.Nil || mutation.Amount == 0 {
		return nil, fmt.Errorf("entry number, account and non-zero amount are required")
	}
	var result model.WalletEntry
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("entry_no = ?", mutation.EntryNo).First(&result).Error; err == nil {
			if result.AccountID != mutation.AccountID || result.Amount != mutation.Amount || result.Type != mutation.Type || result.ReferenceType != mutation.ReferenceType || !sameOptionalUUID(result.ReferenceID, mutation.ReferenceID) {
				return ErrIdempotencyConflict
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var account model.WalletAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "id = ?", mutation.AccountID).Error; err != nil {
			return err
		}
		balance, err := walletBalanceAfter(account.Balance, account.Frozen, mutation.Amount)
		if err != nil {
			return err
		}
		if err := tx.Model(&account).Updates(map[string]any{"balance": balance, "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		result = model.WalletEntry{AccountID: account.ID, EntryNo: mutation.EntryNo, Type: mutation.Type, Amount: mutation.Amount, BalanceAfter: balance, ReferenceType: mutation.ReferenceType, ReferenceID: mutation.ReferenceID, Description: mutation.Description}
		return tx.Create(&result).Error
	})
	return &result, err
}

func walletBalanceAfter(balance, frozen, mutation int64) (int64, error) {
	if balance < 0 || frozen < 0 || frozen > balance {
		return 0, ErrWalletStateInvalid
	}
	result, err := checkedAddInt64(balance, mutation)
	if err != nil {
		return 0, err
	}
	if result < frozen {
		return 0, ErrInsufficientBalance
	}
	return result, nil
}

func sameOptionalUUID(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
