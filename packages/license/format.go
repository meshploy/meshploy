package license

// TokenPrefix namespaces the token format and version. A verifier rejects any
// token whose prefix it does not recognize.
//
// This is part of the wire format, so both halves need it: the issuer
// (meshploy-admin, which holds the private key) builds tokens with it, and the
// verifier here checks it. It lives in the public package precisely so there is
// one definition — a second copy on the issuer side could drift and would
// invalidate every previously issued token.
//
//	mlic-v1.<base64url(claims-json)>.<base64url(signature)>
const TokenPrefix = "mlic-v1"
