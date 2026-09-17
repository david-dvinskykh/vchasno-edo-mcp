# Покрытие API Вчасно.ЕДО v2

Каждый метод публичного API и инструмент MCP, который его вызывает. Составлено по документации «API Вчасно.ЕДО» (редакция с `changed_from/changed_to`, публичными ссылками, импортом подписанных документов и файловыми шаблонами).

## Документы

| Метод API | Инструмент |
|---|---|
| `GET /documents` | `list_documents`, `sync_changed_documents` |
| `GET /documents/<id>` | `get_document` |
| `POST /documents` | `upload_document` |
| `PATCH /documents/<id>/info` | `update_document_info` |
| `PATCH /documents/<id>/recipient` | `set_document_recipient` |
| `PATCH /documents/{id}/recipients` | `set_document_recipients` |
| `PATCH /documents/<id>/access-settings` | `set_document_access` |
| `PATCH /documents/<id>/viewers-settings` | `set_document_viewers` |
| `POST /documents/<id>/flow` | `set_multilateral_route` |
| `GET /documents/<id>/flows` | `get_multilateral_route` |
| `POST /documents/<id>/signers` | `set_document_signers` |
| `GET /incoming-documents` | `list_incoming_documents`, `sync_changed_documents` |
| `POST /documents/statuses` | `get_document_statuses` |
| `POST /documents/mark-as-processed` | `mark_documents_processed` |
| `DELETE /documents/<id>` | `delete_document` |

## Подписи и отправка

| Метод API | Инструмент |
|---|---|
| `GET /documents/<id>/signatures` | `list_signatures` |
| `POST /documents/<id>/signatures` | `add_signature` |
| `POST /documents/<id>/send` | `send_document`, `add_signature` с `send=true` |
| `POST /documents/<id>/reject` | `reject_document` |

## Скачивание

| Метод API | Инструмент |
|---|---|
| `GET /documents/<id>/original` | `download_document` `format=original` |
| `GET /documents/<id>/archive` | `download_document` `format=archive` |
| `GET /documents/<id>/p7s` | `download_document` `format=p7s` |
| `GET /documents/<id>/asic` | `download_document` `format=asic` |
| `GET /documents/<id>/pdf/print` | `download_document` `format=pdf` |
| `POST` + `GET /documents/<id>/xml-to-pdf` | `download_document` `format=xml_pdf` |
| `GET /download-documents` | `get_download_links` |

## Версии и связи

| Метод API | Инструмент |
|---|---|
| `POST /documents/<id>/version` | `upload_document_version` |
| `DELETE /documents/<id>/version/<vid>` | `delete_document_version` |
| `POST /documents/<parent>/child/<child>` | `attach_child_document` |
| `DELETE /documents/<parent>/child/<child>` | `detach_child_document` |

## Удаление по согласию

| Метод API | Инструмент |
|---|---|
| `POST /documents/<id>/delete-requests` | `create_delete_request` |
| `DELETE /documents/<id>/delete-requests` | `cancel_delete_request` |
| `POST /documents/<id>/delete-requests/acceptions` | `accept_delete_request` |
| `POST /documents/<id>/delete-requests/rejections` | `reject_delete_request` |
| `GET /documents/delete-requests` | `list_delete_requests` |
| `POST` / `DELETE /documents/delete-requests/lock-delete` | `lock_document_deletion` (`action=lock` / `unlock`) |

## Комментарии и согласование

| Метод API | Инструмент |
|---|---|
| `GET /documents/comments` | `list_comments` |
| `GET /documents/<id>/comments` | `get_document_comments` |
| `POST /documents/<id>/comments` | `add_comment` |
| `GET /documents/<id>/reviews` | `get_review_state` |
| `GET /documents/<id>/reviews/requests` | `get_review_state` |
| `GET /documents/<id>/reviews/status` | `get_review_state` |
| `POST /documents/<id>/reviews/requests` | `add_reviewer` |
| `DELETE /documents/<id>/reviews/requests` | `remove_reviewer` |

## Структурированные данные

| Метод API | Инструмент |
|---|---|
| `POST /documents/structured-data/extractions` | `start_structured_data` |
| `GET /documents/structured-data/extractions` | `list_structured_data` |
| `GET /documents/<id>/structured-data/download` | `download_structured_data` |

## Ярлыки

| Метод API | Инструмент |
|---|---|
| `GET /tags` | `list_tags` |
| `POST /tags/documents` | `tag_documents` `action=create` |
| `POST /tags/documents/connections` | `tag_documents` `action=assign` |
| `DELETE /tags/documents/connections` | `tag_documents` `action=unassign` |
| `POST /tags/roles` | `tag_employees` `action=create` |
| `POST /tags/roles/connections` | `tag_employees` `action=assign` |
| `DELETE /tags/roles/connections` | `tag_employees` `action=unassign` |
| `GET /tags/<id>/roles` | `list_tag_roles` |

## Дополнительные параметры

| Метод API | Инструмент |
|---|---|
| `GET /fields` | `list_fields` |
| `POST /fields` | `create_field` |
| `PATCH /fields/{id}` | `update_field` |
| `GET /documents/<id>/fields` | `get_document_fields` |
| `POST /documents/<id>/fields` | `set_document_field` |

## Архив

| Метод API | Инструмент |
|---|---|
| `GET /archive/directories` | `list_archive_folders` |
| `POST /archive/scans` | `upload_scan` |
| `POST /documents/archive` | `archive_documents` |
| `DELETE /documents/archive` | `unarchive_documents` |
| `POST /archive/import-signed` (внешний формат) | `import_signed_document` с `original` + `signatures` |
| `POST /archive/import-signed` (внутренний формат) | `import_signed_document` с `container` |
| `POST /archive/import-signed/{id}/visualization` | `upload_archive_visualization` |

## Публичные ссылки

| Метод API | Инструмент |
|---|---|
| `GET /shared-documents/<id>` | `get_public_link` |
| `POST /shared-documents/<id>` | `create_public_link` |
| `PUT /shared-documents/<shared_id>` | `update_public_link` |
| `DELETE /shared-documents/<shared_id>` | `revoke_public_link` |

## Типы документов

| Метод API | Инструмент |
|---|---|
| `GET /document-categories` | `list_document_categories` |
| `POST /document-categories` | `create_document_category` |
| `PATCH /document-categories/<id>` | `rename_document_category` |
| `DELETE /document-categories/<id>` | `delete_document_category` |

## Команды

| Метод API | Инструмент |
|---|---|
| `GET /groups` | `list_groups` |
| `GET /groups/<id>` | `list_groups` с параметром `group` |
| `POST /groups` | `create_group` |
| `PATCH /groups/<id>` | `rename_group` |
| `DELETE /groups/<id>` | `delete_group` |
| `GET /groups/<id>/members` | `group_members` `action=list` |
| `POST /groups/<id>/members` | `group_members` `action=add` |
| `POST /groups/<id>/members/remove` | `group_members` `action=remove` |

## Сценарии и шаблоны

| Метод API | Инструмент |
|---|---|
| `GET /templates` | `list_scenarios` |
| `GET /templates/<id>` | `list_scenarios` с `id` |
| `GET /document-templates` | `list_document_templates` |
| `GET /document-templates/{id}` | `get_document_template` |
| `POST /document-templates/{id}/document` | `create_document_from_template` |

## Облачное подписание Вчасно.КЕП

| Метод API | Инструмент |
|---|---|
| `POST /cloud-signer/sessions/create` | `cloud_sign_create_session` |
| `POST /cloud-signer/sessions/check` | `cloud_sign_check_session` |
| `POST /cloud-signer/sessions/refresh/check` | `cloud_sign_check_refresh_session` |
| `POST /cloud-signer/sessions/refresh` | `cloud_sign_refresh_token` |
| `POST /cloud-signer/sessions/sign-document` | `cloud_sign_document` |

## Личный кабинет

| Метод API | Инструмент |
|---|---|
| `POST /sign-sessions` | `create_sign_session` |

## Сотрудники

| Метод API | Инструмент |
|---|---|
| `GET /roles` | `list_roles` |
| `PATCH /roles/{id}` (права, уведомления, IP) | `update_role` |
| `PATCH /roles/{id}` (`status: active`) | `update_role` с `status=active` |
| `DELETE /roles/{id}` | `delete_role` |
| `POST /invite/coworkers` | `invite_coworkers` |
| `POST /coworker` | `create_coworker` |
| `POST /tokens` | `create_user_tokens` |
| `DELETE /tokens` | `reset_user_tokens` |

## Отчёты, проверки, тарифы

| Метод API | Инструмент |
|---|---|
| `POST /document-actions/request-report` | `request_actions_report` `kind=documents` |
| `POST /user-actions/request-report` | `request_actions_report` `kind=users` |
| `GET /actions/report-status/<id>` | `get_actions_report`, `request_actions_report` с `wait=true` |
| `GET /actions/download-report/<id>` | `get_actions_report`, `request_actions_report` с `wait=true` |
| `POST /check/company` | `check_counterparty` с `edrpou` |
| `POST /check/company/upload` | `check_counterparty` с `file` |
| `GET /company/billing` | `get_billing`, `self_check` |
| `POST /billing/companies/rates/trials` | `activate_integration_trial` |

## Сознательно не реализовано

- **`/api/v1/documents` и `/api/v1/incoming-documents`** — устаревшие версии тех же методов, отличаются отсутствием курсорной пагинации. Используется v2.
- **`roles` в `POST /documents/<id>/signers` и `reviewers_ids`/`signers_ids` в сценариях** — помеченные в документации как устаревшие формы; сервер отправляет актуальные `signer_entities` / `reviewer_entities`.
