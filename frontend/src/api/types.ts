export type Role = 'student' | 'teacher' | 'admin'

export interface User {
  id: string
  email: string
  full_name: string
  role: Role
}

export interface Course {
  id: string
  slug: string
  title: string
  summary: string
  created_by_teacher_id: string
  current_published_version_id?: string | null
  created_at: string
}

export type ResourceType =
  | 'text' | 'image' | 'video' | 'audio' | 'pdf'
  | 'presentation' | 'download' | 'iframe' | 'link' | 'quiz'

export type ProcessingStatus = 'pending' | 'processing' | 'completed' | 'failed'

export interface Resource {
  id: string
  unit_id: string
  stable_id: string
  title: string
  type: ResourceType
  canonical_markdown?: string
  media_url?: string
  object_key?: string
  presigned_upload_url?: string
  is_visible: boolean
  is_mandatory: boolean
  position: number
  processing_status: ProcessingStatus
}

export interface Unit { id: string; module_id: string; title: string; position: number; resources?: Resource[] }
export interface Module { id: string; version_id: string; title: string; description: string; position: number; units?: Unit[] }

export interface CourseVersion {
  id: string
  course_id: string
  version_number: number
  status: 'draft' | 'published' | 'archived'
  passing_score: number
  modules?: Module[]
}

export interface Enrollment {
  id: string
  course_id: string
  status: string
  academic_status: 'enrolled' | 'completed' | 'approved'
  progress_percentage: number
  course_title: string
  course_slug: string
  course_summary: string
}

export interface QuizOption { id: string; option_text: string; position: number }
export interface QuizQuestion { id: string; question_text: string; position: number; points: number; options: QuizOption[] }
export interface QuizSnapshot { id: string; resource_id: string; max_attempts: number; passing_score: number; questions: QuizQuestion[] }
export interface QuizAttempt { id: string; quiz_id: string; attempt_number: number; status: string; score?: number | null; answers: Record<string, string> }
