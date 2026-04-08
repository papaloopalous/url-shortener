package dto

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (r RegisterRequest) Validate() error {
	if r.Email == "" {
		return ValidationError("email is required")
	}
	if len(r.Password) < 8 {
		return ValidationError("password must be at least 8 characters")
	}
	return nil
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (r LoginRequest) Validate() error {
	if r.Email == "" || r.Password == "" {
		return ValidationError("email and password are required")
	}
	return nil
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (r RefreshRequest) Validate() error {
	if r.RefreshToken == "" {
		return ValidationError("refresh_token is required")
	}
	return nil
}

type LogoutRequest struct {
	RefreshToken     string `json:"refresh_token"`
	JTI              string `json:"jti"`
	AccessTTLSeconds int64  `json:"access_ttl_seconds"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type ValidationError string

func (e ValidationError) Error() string { return string(e) }
