package service

import "context"

// UserRPMCache
//
//
//   - RPMCache    —— {accountID}:{min}）。
//   - UserRPMCache —— () ""
//     key {userID}:{groupID}:{min} {userID}:{min}。
type UserRPMCache interface {
	// IncrementUserGroupRPM (user, group)
	//
	IncrementUserGroupRPM(ctx context.Context, userID, groupID int64) (count int, err error)

	// IncrementUserRPM
	//
	IncrementUserRPM(ctx context.Context, userID int64) (count int, err error)

	// GetUserGroupRPM (user, group)
	GetUserGroupRPM(ctx context.Context, userID, groupID int64) (count int, err error)

	// GetUserRPM
	GetUserRPM(ctx context.Context, userID int64) (count int, err error)
}
