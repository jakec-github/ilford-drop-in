-- What the app last published to the Latest tab of the rota sheet (issue #191).
--
-- The sheet is edited in place rather than rewritten: people type Hot food and
-- Collection, and add columns of their own, beside the app's. Keeping those
-- beside the right shift means knowing what the sheet looked like before this
-- publish — which rota, which shift is on which row, how many columns each Role
-- took — and the sheet cannot be trusted to say, because anyone can edit it.
--
-- One row or none. None is a deployment that has never published, and its
-- first publish is treated as a new rota: Latest is archived and rebuilt.
CREATE TABLE published_rota_sheet (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    -- Not a foreign key: the rota Latest shows is a fact about the sheet, and
    -- discarding a rota never touches an allocated one anyway.
    rota_id UUID NOT NULL,
    -- rotasheet.Layout, whole. JSON because nothing queries inside it: it is
    -- read back in one piece and diffed in Go.
    layout JSONB NOT NULL,
    published_at TIMESTAMPTZ NOT NULL
);
