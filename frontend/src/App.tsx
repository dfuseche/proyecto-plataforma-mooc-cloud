import { Route, Routes } from 'react-router-dom'
import Layout from './components/Layout'
import RequireAuth from './components/RequireAuth'
import Catalog from './pages/Catalog'
import CourseDetail from './pages/CourseDetail'
import { Login, Register } from './pages/Auth'
import MyCourses from './pages/MyCourses'
import Learn from './pages/Learn'
import TeacherDashboard from './pages/teacher/Dashboard'
import CourseEditor from './pages/teacher/CourseEditor'
import AdminTeachers from './pages/admin/Teachers'

export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Catalog />} />
        <Route path="cursos/:courseId" element={<CourseDetail />} />
        <Route path="entrar" element={<Login />} />
        <Route path="registro" element={<Register />} />
        <Route path="mis-cursos" element={<RequireAuth><MyCourses /></RequireAuth>} />
        <Route path="aprender/:courseId/:resourceId?" element={<RequireAuth><Learn /></RequireAuth>} />
        <Route path="docente" element={<RequireAuth roles={['teacher', 'admin']}><TeacherDashboard /></RequireAuth>} />
        <Route path="docente/cursos/:courseId" element={<RequireAuth roles={['teacher', 'admin']}><CourseEditor /></RequireAuth>} />
        <Route path="admin/docentes" element={<RequireAuth roles={['admin']}><AdminTeachers /></RequireAuth>} />
        <Route path="*" element={<div className="page narrow"><h1>Página no encontrada</h1><p>La dirección no existe. Vuelve al catálogo desde el menú.</p></div>} />
      </Route>
    </Routes>
  )
}
