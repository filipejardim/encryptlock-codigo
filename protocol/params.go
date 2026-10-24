package protocol

import "time"

const (
	Decimals                   = 8
	MaxSupplyCoins      uint64 = 40_000_000
	BaseUnitsPerCoin    uint64 = 100_000_000
	MaxSupplyBaseUnits         = MaxSupplyCoins * BaseUnitsPerCoin
	IssuanceMonths             = 360
	HalvingPeriodMonths        = 60
)

const TransferAcceptanceWindow = time.Hour

// Transfer fee (white paper, section 8.1): 0,1% of the amount, paid by the
// sender, with a minimum and a maximum. Until fees are linked to a dollar
// reference, the minimum and maximum are fixed in LOCK (test values).
const (
	TransferFeeBasisPoints uint64 = 10        // 10 / 10.000 = 0,1%
	TransferFeeMin         uint64 = 20_000    // 0,0002 LOCK
	TransferFeeMax         uint64 = 1_000_000 // 0,01 LOCK
)

// FeeSplitPeriod is one row of the fee split table (white paper, section 8.3).
type FeeSplitPeriod struct {
	UntilYear      int64  // the row applies while the network is younger than this many years (0 = forever)
	DevFundPercent uint64 // the rest goes to the LockKeepers
}

// FeeSplit: development fund / LockKeepers = 75/25 for the first 5 years,
// then 50/50 for ever (founder's decision of 12 Oct 2026). Reaching the final
// milestone (FinalMilestoneWallets) before the 5 years also makes it 50/50 at
// once. A year is 365 days of network time.
var FeeSplit = []FeeSplitPeriod{
	{UntilYear: 5, DevFundPercent: 75},
	{UntilYear: 0, DevFundPercent: 50},
}

// FinalMilestoneWallets: with this many active wallets every fee is halved
// for ever and the fee split becomes 50/50 (white paper, section 5.4).
const FinalMilestoneWallets = 10_000_000

const YearSeconds int64 = 365 * 24 * 60 * 60

// Security shield (white paper, section 7.2).
const (
	// ApprovalWindow: the second device has 24 hours to approve a transfer to
	// a new contact or above the approval amount; otherwise it goes back.
	ApprovalWindow = 24 * time.Hour
	// LimitLoosenDelay: raising the daily limit or the approval amount only
	// applies 48 hours later (lowering applies at once).
	LimitLoosenDelay = 48 * time.Hour
	// DeviceChangeDelay: changing or removing the second device takes 7 days.
	DeviceChangeDelay = 7 * 24 * time.Hour
	// UnfreezeDelay: unfreezing with only one of the two keys takes 7 days.
	UnfreezeDelay = 7 * 24 * time.Hour
	// DailyLimitWindow is the period the daily limit counts.
	DailyLimitWindow = 24 * time.Hour
)

// Inheritance and recovery by trusted people (white paper, section 7.3).
const (
	// MinInactivityDays: heirs can only claim after at least 1 year without
	// any sign of life (the owner can choose longer).
	// HeirConfirmWindow: once one heir claims, the others have 30 days to
	// confirm; otherwise the claim lapses and can start again.
	HeirConfirmWindow = 30 * 24 * time.Hour
	MinInactivityDays = 365
	// ClaimDelay: after an inheritance claim or an approved recovery, the
	// coins only move 30 days later; the owner can cancel meanwhile.
	ClaimDelay = 30 * 24 * time.Hour
	// PlanChangeDelay: changing heirs or trusted people takes 7 days.
	PlanChangeDelay = 7 * 24 * time.Hour
	// Trusted people: at least 2, and at least 2 must approve a recovery.
	MinGuardians         = 2
	MinGuardianThreshold = 2
	MaxHeirs             = 10
	MaxGuardians         = 10
)

// Sovereign Vault (white paper, section 7.4).
const (
	// VaultMinDelayDays: a vault's outgoing transfers wait at least 7 days.
	VaultMinDelayDays = 7
	// VaultMaxDelayDays: and at most 1 year.
	VaultMaxDelayDays = 365
	// MaxLockYears: coins can be locked for at most 10 years.
	MaxLockYears = 10
	// MinLockUnits: a lock holds at least 0,01 LOCK, and an account has at
	// most MaxLocksPerAccount locks at once (locks are free: without this,
	// a million 1-unit locks would cost almost nothing and fill the network).
	MinLockUnits       = 1_000_000
	MaxLocksPerAccount = 100
)

// Bech32Prefix: every EncryptLock address starts with lock1...
const Bech32Prefix = "lock"

// Market: LOCK ↔ USDC on Polygon (white paper, section 9; EncryptLock_trocas_USDC.md).
const (
	// SwapFeeBasisPoints: 25 / 10.000 = 0,25%, paid in LOCK by the seller.
	SwapFeeBasisPoints uint64 = 25
	// SwapPayWindow: after a sell offer is taken, the buyer locks the USDC on
	// Polygon with a deadline at most this far ahead.
	SwapPayWindow = 30 * time.Minute
	// SwapBuyerPayWindow: when a seller takes a buy offer, the buyer (who may
	// be asleep) gets a notice and has this long to pay.
	SwapBuyerPayWindow = 12 * time.Hour
	// SwapReserveMargin: the reservation lasts this much longer than the
	// payment deadline, so a payment seen at the last moment still settles.
	SwapReserveMargin = 30 * time.Minute
	// SwapMaxOfferDays: an offer lasts at most this long (0 = until canceled).
	SwapMaxOfferDays = 365
	// SwapMinPriceUSDC: only trades of at least 1 USDC count for the price,
	// so a tiny trade cannot set it.
	SwapMinPriceUSDC uint64 = 1_000_000
	// SwapPriceWindow: the average price is over the last 24 hours.
	SwapPriceWindow = 24 * time.Hour
	// Fair use (security review, 11 Oct): taking an offer and placing a buy
	// offer are free, so one account has at most SwapMaxOpenOffers open
	// offers and SwapMaxOpenTrades trades waiting for its payment, every
	// offer is worth at least SwapMinPriceUSDC, and a buyer who did not pay
	// waits SwapUnpaidWait before taking another offer. Nobody can then hold
	// every offer reserved, or fill the market with fake offers.
	SwapMaxOpenOffers = 10
	SwapMaxOpenTrades = 2
	SwapUnpaidWait    = 6 * time.Hour
)

// LockKeepers switched on (founder's decision, 6 Oct): everyone switched on
// works and earns, paid by time on × guarantee; a drawn group, always
// rotating, signs the blocks.
const (
	// HeartbeatEvery: the program next to a LockKeeper says "I am on" this
	// often. HeartbeatMinGap: a heartbeat sooner than this is refused.
	HeartbeatEvery  = 5 * time.Minute
	HeartbeatMinGap = 4 * time.Minute
	// OnlineWindow: a LockKeeper counts as on while its last heartbeat is
	// this recent. Time on between two heartbeats further apart than this is
	// not counted (it was off).
	OnlineWindow = 10 * time.Minute
	// PayEvery: the earnings are shared out among those switched on this often.
	PayEvery = time.Hour
	// Signing group (founder's decision, 9 Oct): every seat is drawn by lot,
	// all seats equal (1 chip = 1 vote: the guarantee counts for the
	// earnings, not for the power to sign).
	//
	// SigningSeats: the group starts with this many seats and grows with the
	// network, one seat per OnlinePerSeat LockKeepers on, up to
	// MaxSigningSeats (1 block per second needs a signature from 2/3 of them;
	// more would need slower blocks). With fewer on than seats, all sign.
	SigningSeats    = 100
	MaxSigningSeats = 300
	OnlinePerSeat   = 10
	// StableAfter: only LockKeepers on without a break (and not taken out
	// for missing blocks) for this long are drawn, while there are enough.
	StableAfter = time.Hour
	// RotateEvery: this often 1 in RotateDivisor seats (at least one)
	// changes hands, the longest-seated first: ~10% of the group per hour,
	// always rotating, never all at once, so everyone gets a turn and the
	// blocks never stop.
	RotateEvery   = 6 * time.Minute
	RotateDivisor = 100
	// Start phase (founder's decision, 20 Oct): while fewer than
	// StableKeepers LockKeepers are on and stable (on at least StableWeek in
	// the last 7 days), the computers that started the network hold more
	// than 2/3 of the power to sign. Then a home computer switched off
	// without warning never stops the blocks. From StableKeepers on, every
	// computer counts the same again, by itself.
	StableKeepers = 20
	StableWeek    = 100 * time.Hour
)
