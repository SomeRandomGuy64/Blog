-- Tokyo (標準語) mode of the Kyōtsu trainer (api/hyojungo.go): Kotu's minimal pairs, NHK
-- recordings served only to accounts with access. Additive only.
--   users.hyojungo         an admin gave this account Tokyo-mode access (admins always have it)
--   kyotsu_sessions.mode   which pitch the session trained: 'keihan' (every older session) or
--                          'tokyo'. Tokyo sessions fill Track's Kotu % and log their time under
--                          Phonetic training › Kotu.
ALTER TABLE users ADD COLUMN hyojungo INTEGER NOT NULL DEFAULT 0;
ALTER TABLE kyotsu_sessions ADD COLUMN mode TEXT NOT NULL DEFAULT 'keihan' CHECK (mode IN ('keihan', 'tokyo'));
