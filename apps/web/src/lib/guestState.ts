import type { Guest } from "@/api/types";

/**
 * What a guest shows about itself, once its runtime state, the CRM and the
 * guest agent have been weighed against each other.
 *
 * It is NOT `GuestStatus`. The payload says three things about a guest and this
 * says five, because two of them come from elsewhere: whether the HA manager
 * gave up on it, and whether it has an agent configured. The sidebar tree
 * paints one glyph per value.
 */
export type GuestIndicator =
  | "running"
  | "stopped"
  | "template"
  | "troubled"
  | "agentless";

/**
 * The CRM states that mean something went wrong, as opposed to a state somebody
 * chose or a move in progress.
 *
 * "migrate" and "relocate" are deliberately absent: they are the manager doing
 * its job, and painting a planned move red would cry wolf through every
 * maintenance window. "disabled", "ignored" and "stopped" are decisions, not
 * incidents. What is left is the pair worth seeing from a panel that is always
 * on screen: a service the manager gave up on, and one whose node is being
 * fenced.
 */
const TROUBLED_HA_STATES: ReadonlySet<string> = new Set(["error", "fence"]);

/** Whether the CRM's own word for a guest denotes an incident. */
export function isTroubledHaState(state: string | null): boolean {
  return state !== null && TROUBLED_HA_STATES.has(state);
}

/**
 * Decides what one guest shows, in one place.
 *
 * THE ORDER IS THE RULE, and it is written here rather than left to the order
 * of the branches in a component:
 *
 *  1. An incident wins over everything. A VM the manager gave up on is worth
 *     seeing whether it is running or not.
 *  2. A template is next: it has no runtime state to report, and no agent to
 *     miss.
 *  3. A missing agent is only said of a RUNNING VM. A stopped machine has no
 *     agent answering either way, so flagging it would mark half a fleet for a
 *     fact that says nothing about it.
 *  4. What is left is the runtime state, which is what the tree has always
 *     shown.
 *
 * `agent === null` — unknown — never reaches step 3: not asked yet, not
 * askable, or asked and unanswered all mean the same thing, and none of them
 * means "no agent".
 */
export function guestIndicator(guest: Guest): GuestIndicator {
  if (isTroubledHaState(guest.haState)) return "troubled";
  if (guest.status === "template") return "template";
  if (guest.status === "running" && guest.agent === false) return "agentless";
  return guest.status;
}
