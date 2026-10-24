package domain

import "errors"

var (
	ErrUserNotFound          = errors.New("usuario no encontrado")
	ErrUserAlreadyExists     = errors.New("el correo electrónico ya está registrado")
	ErrInvalidCredentials    = errors.New("credenciales inválidas")
	ErrEmailNotVerified      = errors.New("el correo electrónico no ha sido verificado")
	ErrUserSuspended         = errors.New("la cuenta del usuario se encuentra suspendida")
	ErrSessionExpired        = errors.New("la sesión ha expirado o fue revocada")
	ErrUnauthorized          = errors.New("no autorizado")
	ErrForbidden             = errors.New("acceso denegado para su rol")
	ErrLastAdminProtection   = errors.New("no es posible desactivar o degradar al último administrador activo")
	ErrWeakPassword          = errors.New("la contraseña debe tener al menos 8 caracteres")
	ErrInvalidToken          = errors.New("el token suministrado es inválido o ha expirado")
)
