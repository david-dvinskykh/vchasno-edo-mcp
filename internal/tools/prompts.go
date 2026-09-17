package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP prompts: ready-made recipes that turn a business question ("what have
// they not signed yet", "send this act to that counterparty") into the exact
// sequence of calls this server needs, so a client can offer them as slash
// commands and the model does not have to rediscover the flow.

type promptDef struct {
	name, title, desc string
	args              []*mcp.PromptArgument
	build             func(a map[string]string) string
}

func arg(name, desc string, required bool) *mcp.PromptArgument {
	return &mcp.PromptArgument{Name: name, Description: desc, Required: required}
}

func (d *Deps) registerPrompts(srv *mcp.Server) {
	defs := []promptDef{
		{"onboard_company", "Осмотреться в компании",
			"Orientation: whether the connection works, which tariff and limits are in force, who works here, what labels, teams, parameters and document types exist, and what the document flow currently looks like. Run once at the start of a session.",
			nil, d.promptOnboard},
		{"outgoing_overview", "Что мы отправили",
			"State of the outgoing documents over a period: how many are in each status, what is waiting for counterparties, what was rejected and why, what is finished.",
			[]*mcp.PromptArgument{arg("date_from", "Start of the period, YYYY-MM-DD", false), arg("date_to", "End of the period, YYYY-MM-DD", false)},
			d.promptOutgoing},
		{"incoming_inbox", "Входящие — что требует действий",
			"Work the inbox: unprocessed incoming documents, which of them wait for our signature, which are stuck in internal approval, and what to do with each.",
			[]*mcp.PromptArgument{arg("date_from", "Only documents received on or after this date, YYYY-MM-DD", false)},
			d.promptInbox},
		{"send_document_flow", "Отправить документ контрагенту",
			"The full send: check the counterparty, upload the file with its metadata, set signers, sign and send, then confirm the resulting status.",
			[]*mcp.PromptArgument{arg("file_path", "Path to the file to send", true), arg("edrpou", "Counterparty ЄДРПОУ/ІПН", true),
				arg("email", "Counterparty email", false), arg("category", "Document type, e.g. Рахунок or Акт наданих послуг", false),
				arg("amount", "Amount in hryvnias", false), arg("number", "Document number", false)},
			d.promptSend},
		{"chase_unsigned", "Кто не подписал",
			"Find every document hanging unsigned longer than it should, grouped by counterparty, with how long each has been waiting and what the next step is for each.",
			[]*mcp.PromptArgument{arg("days", "Consider a document stuck after this many days (default 7)", false)},
			d.promptChase},
		{"counterparty_dossier", "Досье контрагента",
			"Everything about one counterparty: what we sent them, what they sent us, what is signed, what is rejected, the amounts and the open items.",
			[]*mcp.PromptArgument{arg("edrpou", "Counterparty ЄДРПОУ/ІПН", true), arg("date_from", "Start of the period, YYYY-MM-DD", false)},
			d.promptCounterparty},
		{"monthly_report", "Отчёт за месяц",
			"A month of document flow: counts and amounts by direction, type and status, the top counterparties, what is still unfinished, and the action-history report as a file.",
			[]*mcp.PromptArgument{arg("month", "Month, YYYY-MM", true)},
			d.promptMonthly},
		{"sync_integration", "Инкрементальная синхронизация",
			"One cycle of a reliable integration: pull the changes of a window, mark what was imported, and hand back the start of the next window.",
			[]*mcp.PromptArgument{arg("since", "Start of the window, ISO 8601, from the previous run", true), arg("direction", "outgoing, incoming or both", false)},
			d.promptSync},
		{"troubleshoot_access", "Что-то не работает",
			"Diagnose a failing call: the tariff, the token, the employee's permissions and the limits, in the order that tells them apart.",
			[]*mcp.PromptArgument{arg("problem", "What failed and with which error, in free text", false)},
			d.promptTrouble},
	}
	for _, p := range defs {
		p := p
		srv.AddPrompt(&mcp.Prompt{Name: p.name, Title: p.title, Description: p.desc, Arguments: p.args},
			func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				args := map[string]string{}
				if req.Params != nil {
					for k, v := range req.Params.Arguments {
						args[k] = strings.TrimSpace(v)
					}
				}
				for _, a := range p.args {
					if a.Required && args[a.Name] == "" {
						return nil, fmt.Errorf("prompt %s: argument %q is required", p.name, a.Name)
					}
				}
				return &mcp.GetPromptResult{Description: p.desc,
					Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: p.build(args)}}}}, nil
			})
	}
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func monthBounds(month string) (from, to string) {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return month + "-01", month + "-28"
	}
	return t.Format("2006-01-02"), t.AddDate(0, 1, -1).Format("2006-01-02")
}

func (d *Deps) promptOnboard(map[string]string) string {
	return `Осмотрись в компании «Вчасно.ЕДО» и дай короткую сводку.

1. ` + "`self_check`" + ` with deep=true — is the connection healthy, is the API tariff active, which endpoints answer.
2. Read the resources ` + "`vchasno://company/overview`" + ` and ` + "`vchasno://company/reference`" + ` — tariffs and limits, employees, teams, labels, extra parameters, the company's own document types.
3. ` + "`list_documents`" + ` with limit=50 and ` + "`list_incoming_documents`" + ` with limit=50 — what the current flow looks like.
4. ` + "`list_incoming_documents processed=false`" + ` — how much is waiting to be handled.

Ответ: одним экраном — тариф и остаток лимитов, сколько сотрудников и какие у них роли, сколько документов в работе по каждому статусу, что требует внимания прямо сейчас. Суммы — в гривнах. Если API закрыт (access_denied) — скажи прямо, что нужен тариф «Інтеграція», и не перечисляй остальное.`
}

func (d *Deps) promptOutgoing(a map[string]string) string {
	from := orDefault(a["date_from"], time.Now().AddDate(0, -1, 0).Format("2006-01-02"))
	to := orDefault(a["date_to"], time.Now().Format("2006-01-02"))
	return fmt.Sprintf(`Разбери исходящие документы за период %s … %s.

1. `+"`list_documents`"+` date_from=%s date_to=%s limit=100 pages=5 with=["recipients"] — всё за период.
2. Разложи по статусам: 7000/7001 (ещё не отправлены), 7002/7004 (у контрагента), 7003/7007 (недоподписаны у нас), 7008 (готовы), 7006 (отклонены).
3. Для отклонённых — `+"`get_document_comments`"+` по каждому, чтобы взять причину.
4. Для висящих в 7004 — посчитай, сколько дней прошло с date_created.

Ответ: таблица по статусам (количество и сумма в гривнах), отдельно список отклонённых с причинами и список «зависших» дольше недели с контрагентом и датой. В конце — что стоит сделать: что дослать, что переподписать, к кому обратиться.`, from, to, from, to)
}

func (d *Deps) promptInbox(a map[string]string) string {
	from := orDefault(a["date_from"], time.Now().AddDate(0, 0, -30).Format("2006-01-02"))
	return fmt.Sprintf(`Разбери входящие документы «Вчасно» и скажи, что с ними делать.

1. `+"`list_incoming_documents`"+` processed=false date_created_from=%s limit=100 pages=5 with=["recipients","fields"].
2. Раздели: ждут нашей подписи (7002, 7004, 7007, 7010), уже завершены (7008), отклонены (7006), просто получены (7000/7001).
3. Для тех, что ждут подписи, проверь внутреннее согласование: `+"`get_review_state`"+` по каждому — если статус pending и is_required=true, подписать нельзя, пока не согласуют.
4. Если чего-то не хватает для решения (сумма, номер, тип) — `+"`get_document`"+` full=true по этому документу.

Ответ: список по группам, в каждой — контрагент, тип, номер, сумма в гривнах, дата, и конкретное следующее действие (подписать, отклонить, дождаться согласования, просто отметить обработанным). Ничего не подписывай и не отклоняй сам — только предложи. Отметить обработанными (`+"`mark_documents_processed`"+`) предложи отдельным шагом с перечнем id.`, from)
}

func (d *Deps) promptSend(a map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Отправь документ контрагенту через «Вчасно.ЕДО».\n\nФайл: %s\nЄДРПОУ контрагента: %s\n", a["file_path"], a["edrpou"])
	if a["email"] != "" {
		fmt.Fprintf(&b, "Email контрагента: %s\n", a["email"])
	}
	for _, f := range []struct{ label, key string }{{"Тип документа", "category"}, {"Сумма (грн)", "amount"}, {"Номер", "number"}} {
		if a[f.key] != "" {
			fmt.Fprintf(&b, "%s: %s\n", f.label, a[f.key])
		}
	}
	b.WriteString(`
Порядок:
1. ` + "`check_counterparty`" + ` по ЄДРПОУ. Если не зарегистрирован — скажи об этом и остановись: документ ему придёт, но подписать он его не сможет, пока не зарегистрируется.
2. ` + "`upload_document`" + ` с файлом и всеми реквизитами, что есть. Если email контрагента известен — передай его: документ сразу станет 7001 (готов к отправке).
3. Проверь статус в ответе. Если 7000 — не хватает контрагента, добавь его через ` + "`set_document_recipient`" + `.
4. Подпись: у этого сервера нет приватных ключей. Спроси, как подписывать — готовым .p7s (` + "`add_signature`" + `) или облачным ключом Вчасно.КЕП (` + "`cloud_sign_create_session`" + ` → подтверждение владельцем → ` + "`cloud_sign_check_session`" + ` → ` + "`cloud_sign_document`" + `). Не подписывай без явного согласия — это юридически значимое действие.
5. После подписи — ` + "`send_document`" + `.
6. ` + "`get_document`" + ` и покажи итоговый статус и ссылку на документ.

Отчёт: одной строкой — id документа, статус с расшифровкой, ссылка, и что происходит дальше (ждём подпись контрагента / ушёл на первую подпись).`)
	return b.String()
}

func (d *Deps) promptChase(a map[string]string) string {
	days := orDefault(a["days"], "7")
	return fmt.Sprintf(`Найди документы, которые зависли без подписи дольше %s дней.

1. `+"`list_documents`"+` status=waiting_for_counterparty limit=100 pages=5 with=["recipients"] — ждут контрагента.
2. `+"`list_documents`"+` status=sent_for_first_signature limit=100 pages=5 with=["recipients"] — ушли на первую подпись контрагенту.
3. `+"`list_documents`"+` status=owner_partially_signed limit=100 pages=5 — недоподписаны с нашей стороны, это наша задача, а не контрагента.
4. По каждому посчитай возраст от date_created (или date_delivered, если он есть) и отбрось всё моложе %s дней.
5. Для самых старых — `+"`get_document`"+` full=true: посмотри, видел ли контрагент документ (is_delivered) и нет ли комментариев.

Ответ: сгруппируй по контрагенту. В каждой группе — документ, тип, номер, сумма в гривнах, сколько дней висит, видел ли контрагент. Отдельно — то, что зависло у нас самих, это чинится без переписки. В конце — кому писать и по каким документам.`, days, days)
}

func (d *Deps) promptCounterparty(a map[string]string) string {
	from := orDefault(a["date_from"], time.Now().AddDate(-1, 0, 0).Format("2006-01-02"))
	return fmt.Sprintf(`Собери досье по контрагенту %s за период с %s.

1. `+"`check_counterparty`"+` edrpou=%s — зарегистрирован ли он в «Вчасно».
2. `+"`list_documents`"+` recipient_edrpou=%s date_from=%s limit=100 pages=10 with=["recipients"] — что мы ему отправляли.
3. `+"`list_incoming_documents`"+` edrpou_owner=%s date_created_from=%s limit=100 pages=10 — что он отправлял нам.
4. Для отклонённых с обеих сторон — `+"`get_document_comments`"+` за причинами.

Ответ: сводка по контрагенту — сколько документов в каждую сторону, на какие суммы (в гривнах), сколько завершено, сколько в работе, сколько отклонено и почему. Отдельно — открытые позиции: что ждёт его подписи и что ждёт нашей. Крупные суммы и давние зависания вынеси отдельно.`, a["edrpou"], from, a["edrpou"], a["edrpou"], from, a["edrpou"], from)
}

func (d *Deps) promptMonthly(a map[string]string) string {
	from, to := monthBounds(a["month"])
	return fmt.Sprintf(`Сделай отчёт по документообороту за %s (%s … %s).

1. `+"`list_documents`"+` date_from=%s date_to=%s limit=100 pages=10 with=["recipients"] — исходящие.
2. `+"`list_incoming_documents`"+` date_created_from=%s date_created_to=%s limit=100 pages=10 with=["recipients"] — входящие.
3. `+"`get_billing`"+` — сколько лимитов израсходовано за счёт этого объёма.
4. `+"`request_actions_report`"+` kind=documents date_from=%s date_to=%s wait=true — файл с историей действий (период до 30 дней, так что для длинного месяца раздели на два запроса).

Ответ:
— количество и сумма (в гривнах) по направлениям;
— разбивка по типам документов и по статусам;
— топ-10 контрагентов по количеству и по сумме;
— что не завершено на конец месяца и почему;
— остаток лимитов тарифа;
— путь к файлу отчёта о действиях.`, a["month"], from, to, from, to, from, to, from, to)
}

func (d *Deps) promptSync(a map[string]string) string {
	dir := orDefault(a["direction"], "both")
	return fmt.Sprintf(`Прогони один цикл инкрементальной синхронизации с «Вчасно».

1. `+"`sync_changed_documents`"+` changed_from=%s direction=%s limit=100 pages=10 with=["recipients"].
   Окно идемпотентно: повторный запрос по тому же окну вернёт то же самое, поэтому при сбое цикл можно просто повторить. Не используй has_changed — он одноразовый.
2. Разложи полученное: новые документы, изменившие статус, завершённые, отклонённые.
3. Для того, что действительно обработано на нашей стороне, предложи `+"`mark_documents_processed`"+` с перечнем id (до 500 за раз).
4. Верни next_window_start из ответа — со следующего раза начинать с него минус 5 минут, потому что изменения попадают в индекс с задержкой.

Ответ: что изменилось (по группам), список id для отметки обработанными и точное значение начала следующего окна.`, a["since"], dir)
}

func (d *Deps) promptTrouble(a map[string]string) string {
	problem := orDefault(a["problem"], "(не указано, что именно не работает)")
	return fmt.Sprintf(`Разберись, почему не работает обращение к «Вчасно.ЕДО».

Проблема: %s

По порядку, от общего к частному:
1. `+"`self_check`"+` deep=true — он отделяет «токен не принят» от «нет тарифа» от «конкретный метод закрыт правами».
2. Если ответ access_denied — дело в тарифе, а не в токене и не в правах: нужен активный «Інтеграція» или «AI Інтеграція». Разовый 30-дневный триал включается `+"`activate_integration_trial`"+`, но только один раз за всю жизнь компании — предложи, не делай сам.
3. Если login_required — токен не передан или отозван; его перевыпускают в настройках сотрудника в кабинете «Вчасно».
4. Если падает только один метод — это права роли. `+"`list_roles`"+` покажет сотрудника, ресурс `+"`vchasno://guide/permissions`"+` — какой флаг за что отвечает, `+"`update_role`"+` его меняет.
5. Если 429 — упёрлись в 10 запросов/секунду на компанию, и лимит общий со всеми её интеграциями.
6. Если 400 на изменении документа — почти всегда статус: реквизиты замораживаются с 7003, контрагента нельзя менять после его подписи, публичная ссылка живёт только для 7000. Ресурс `+"`vchasno://guide/statuses`"+` описывает, что в каком статусе разрешено.
7. Если упирается в лимиты объёма — `+"`get_billing`"+`.

Ответ: назови причину прямо, без перебора версий, и скажи, что именно сделать. Если причина требует действий в кабинете или денег — скажи это отдельной строкой.`, problem)
}
