package domain

const (
	OrganizationCurrencyUSD = "USD"

	OrganizationMoneyScale = 8
)

const (
	OrganizationFundingAuthorizationPlatformAdminJWT = "platform_admin_jwt"
	OrganizationFundingAuthorizationOrganizationJWT  = "organization_admin_jwt"
	OrganizationFundingAuthorizationSystem           = "system_execution"
)

const (
	OrganizationFundingOperationCredit               = "organization_credit"
	OrganizationFundingOperationEqualAllocation      = "equal_allocation"
	OrganizationFundingOperationCustomAllocation     = "custom_allocation"
	OrganizationFundingOperationReclaim              = "allocation_reclaim"
	OrganizationFundingOperationMemberRemovalReclaim = "member_removal_reclaim"
	OrganizationFundingOperationReservation          = "reservation"
	OrganizationFundingOperationConsumption          = "consumption"
	OrganizationFundingOperationRelease              = "release"
)

const (
	OrganizationLedgerBucketUnallocated         = "organization_unallocated"
	OrganizationLedgerBucketAllocationAvailable = "allocation_available"
	OrganizationLedgerBucketAllocationReserved  = "allocation_reserved"
)
