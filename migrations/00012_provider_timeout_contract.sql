-- +goose Up
-- +goose NO TRANSACTION

ALTER TABLE `provider_configurations`
  DROP CHECK `chk_provider_configurations_limits`,
  ADD CONSTRAINT `chk_provider_configurations_limits` CHECK (((`timeout_ms` between 1000 and 300000) and (`max_results` between 1 and 10000)));
