-- Distingue un guardado automático (autosave, mientras se edita) de un
-- guardado explícito (PUT completo). Sin esta columna no había forma de
-- comprobar que el autosave funciona: se veía igual que cualquier UPDATE.
ALTER TABLE course_resources
    ADD COLUMN IF NOT EXISTS last_autosaved_at TIMESTAMP WITH TIME ZONE;
