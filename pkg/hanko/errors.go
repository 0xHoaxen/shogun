package hanko

import "errors"

// Sentinel errors returned (wrapped) by Sign and Verify. Callers match them
// with errors.Is and map them to stable gRPC ErrorInfo reasons.
var (
	// ErrMalformed means the token is not a well-formed v4.public PASETO.
	ErrMalformed = errors.New("hanko: malformed token")
	// ErrUnknownKey means the footer kid is absent from the trusted key set.
	ErrUnknownKey = errors.New("hanko: unknown key id")
	// ErrSignature means the signature does not verify under the key for kid.
	ErrSignature = errors.New("hanko: invalid signature")
	// ErrExpired means the token is past its expiry time.
	ErrExpired = errors.New("hanko: token expired")
	// ErrNotYetValid means the token was issued in the future.
	ErrNotYetValid = errors.New("hanko: token not yet valid")
	// ErrAudience means the audience claim is not the expected one.
	ErrAudience = errors.New("hanko: audience mismatch")
	// ErrIssuer means the issuer claim is not the expected one.
	ErrIssuer = errors.New("hanko: issuer mismatch")
	// ErrDraftMismatch means draft_id or version differ from what is expected.
	ErrDraftMismatch = errors.New("hanko: draft or version mismatch")
	// ErrHashMismatch means body_sha256 or rcpt_sha256 differ from expected.
	ErrHashMismatch = errors.New("hanko: hash mismatch")
	// ErrInvalidClaims means the claims are incomplete or inconsistent.
	ErrInvalidClaims = errors.New("hanko: invalid claims")
	// ErrInvalidExpected means the Expected values are incomplete; Verify
	// refuses to run without every pin set.
	ErrInvalidExpected = errors.New("hanko: invalid expected values")
	// ErrInvalidKey means a signing or verification key is unusable.
	ErrInvalidKey = errors.New("hanko: invalid key")
)
