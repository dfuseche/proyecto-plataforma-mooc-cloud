import { Link, NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'

export default function Layout() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const isTeacher = user?.role === 'teacher' || user?.role === 'admin'

  return (
    <>
      <a href="#contenido" className="skip">Saltar al contenido</a>
      <header className="topbar">
        <Link to="/" className="brand" aria-label="Aula MOOC, inicio">
          <svg viewBox="0 0 32 32" width="28" height="28" aria-hidden="true">
            <rect width="32" height="32" rx="7" fill="#E8A317" />
            <path d="M9 22V10l7 8 7-8v12" fill="none" stroke="#14213D" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          <span>Aula</span>
        </Link>
        <nav aria-label="Principal">
          <NavLink to="/" end>Cursos</NavLink>
          {user && <NavLink to="/mis-cursos">Mis cursos</NavLink>}
          {isTeacher && <NavLink to="/docente">Docencia</NavLink>}
        </nav>
        <div className="account">
          {user ? (
            <>
              <span className="who" title={user.email}>{user.full_name}</span>
              <button className="btn btn-quiet-inv" onClick={async () => { await logout(); navigate('/') }}>Salir</button>
            </>
          ) : (
            <>
              <Link to="/entrar" className="btn btn-quiet-inv">Entrar</Link>
              <Link to="/registro" className="btn btn-accent">Crear cuenta</Link>
            </>
          )}
        </div>
      </header>
      <main id="contenido" tabIndex={-1}>
        <Outlet />
      </main>
    </>
  )
}
