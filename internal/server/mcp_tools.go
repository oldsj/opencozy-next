package server

// The assistant's MCP tools. Each tool is one operation of the API, run in-process as the
// credential's request, so it gets exactly that operation's authorization, validation,
// idempotency, locking and rules (design §9). A tool is listed only when the credential holds
// the operation's x-scopes. To add a tool: give the operation `assistant` in x-actors and a scope
// in x-scopes, then add a line here; TestMCPToolsMatchTheirOperations checks the pairing.
//
// Not yet here, on purpose:
//   - TODO(slice 9, TV gate #56): get_tvs, allow_tv, end_tv_session, open_tvs / extend_tvs /
//     close_tvs. When the TV operations merge, give them `assistant` with tv:read / tv:control
//     and add their lines. Until then no TV tool exists (none pretends to work).
//   - Redeeming, undoing points, fulfilling or cancelling redemptions, and managing children,
//     chores, rewards, devices, the household or credentials stay with the adult's own sign-in.

type mcpTool struct {
	Name string
	// Op is the operationId the tool runs.
	Op string
	// Description comes first; the operation's own description follows it.
	Description string
}

var mcpTools = []mcpTool{
	{"describe_access", "getOwnCredential", "What this assistant may do: its scopes and the children it acts for. Call it first if unsure."},
	{"get_household", "getHousehold", "The household's name, time zone and settings."},
	{"list_children", "listChildren", "The children with their IDs, names, colours and points. Use the IDs everywhere; names can repeat."},
	{"get_child", "getChild", "One child by ID, with their points."},
	{"get_today", "getToday", "Today for every child, as the wall display shows it: chores and routines with their state and points."},
	{"complete_chore", "completeChore", "Mark one of today's chores done as the parent. It counts at once and its points land; no approval is needed."},
	{"check_routine_step", "checkRoutineStep", "Check or uncheck a step of today's routine as the parent. The first full completion awards its points."},
	{"get_inbox", "getInbox", "What waits for a parent's decision, oldest first: finished chores and routines, reward redemptions, requests for more time. Each item has the version to decide it with."},
	{"approve_inbox_item", "approveInboxItem", "Say yes to an inbox item. Confirm with the parent first unless they clearly asked. For a request for more time use answer_time_request."},
	{"reject_inbox_item", "rejectInboxItem", "Say no to an inbox item, with an optional reason the child sees. Confirm with the parent first unless they clearly asked."},
	{"answer_time_request", "answerInboxItem", "Answer a child's request for more iPad time."},
	{"get_points_history", "getPointsHistory", "A child's points history, newest first."},
	{"list_rewards", "listRewards", "The rewards children can spend points on."},
	{"list_redemptions", "listRedemptions", "Rewards the children have redeemed and whether they have been given yet."},
	{"get_calendar", "getCalendar", "The family calendar: events between two household days, and who each is for."},
	{"get_meals", "getMealWeek", "The meal plan for a week."},
	{"get_screen_time", "getScreenTime", "A child's iPad time: what is banked, what is running or queued, and their allowed hours now."},
	{"grant_screen_time", "grantScreenTime", "Give a child iPad time as the parent. It is banked until they use it, unless started now."},
	{"end_screen_time", "endScreenTime", "End a child's iPad time early; the iPad locks again."},
}

const mcpInstructions = `OpenCozy tools for a parent's assistant. You act as the parent who made this credential: what you
do is recorded as that parent, via this assistant. describe_access shows what you may do.
Never pass a household: it comes from the credential. Identify children by ID from list_children (names can
repeat), and items by ID from the latest read. Every tool that changes something needs a requestId: a new
UUID for each intended action. If an answer is lost, retry with the same requestId and arguments to get the
recorded outcome without doing it twice. Confirm with the parent before approving or rejecting anything they
did not clearly ask for. Errors come back as structuredContent.error {code, status, message}.`
