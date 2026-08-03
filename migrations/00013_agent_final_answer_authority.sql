-- +goose Up
-- +goose NO TRANSACTION

ALTER TABLE `agent_runs`
  ADD COLUMN `final_answer` text AFTER `final_diagnosis`,
  ADD CONSTRAINT `chk_agent_runs_final_answer` CHECK ((
    `final_answer` is null OR char_length(trim(`final_answer`)) between 1 and 16000
  ));

UPDATE `agent_runs`
SET `final_answer` = NULLIF(trim(JSON_UNQUOTE(JSON_EXTRACT(`final_diagnosis`, '$.answer'))), '')
WHERE `final_answer` is null
  AND JSON_TYPE(`final_diagnosis`) = 'OBJECT'
  AND JSON_CONTAINS_PATH(`final_diagnosis`, 'one', '$.answer');

UPDATE `agent_runs`
SET `final_diagnosis` = NULL
WHERE JSON_TYPE(`final_diagnosis`) = 'OBJECT'
  AND JSON_LENGTH(`final_diagnosis`) = 1
  AND JSON_CONTAINS_PATH(`final_diagnosis`, 'one', '$.answer');
