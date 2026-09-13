/**
 * Turns a failure of the API layer into the sentence an operator reads.
 *
 * The backend classifies and reports in English — `not found`, `upstream
 * unavailable` — and the wording is the frontend's job, exactly as it is for
 * `formatErrorKind` in format.ts. What is new here is that the *class* of the
 * failure is read too: a deleted VM, an unreachable cluster and a stopped
 * daemon are three different errands, and telling an operator to check that
 * moxyd is running when moxyd has just answered `404` costs an evening.
 *
 * The table below is the whole contract: one entry per class, no sentence built
 * inside a component.
 */
import { ApiParseError, ApiRequestError } from "@/api/client";

/**
 * Why a screen could not be filled.
 *
 * Mirrors what the API layer can actually produce: the statuses
 * `internal/server/detail.go` writes, plus the two failures that never carry
 * one — a request that reached no server at all, and a body that was not the
 * JSON we asked for.
 */
export type FailureKind =
  | "unreachable"
  | "notFound"
  | "forbidden"
  | "upstream"
  | "timeout"
  | "unsupported"
  | "invalid"
  | "internal"
  | "unreadable"
  | "unknown";

export interface FailureExplanation {
  kind: FailureKind;
  /** Heading of the error view, French, sentence case. */
  title: string;
  /** The sentence under it: what happened, and what to do about it. */
  body: string;
  /**
   * Whether going back is the way out.
   *
   * True for a vanished object only: retrying a node that no longer exists
   * will keep failing, while the overview still says what the cluster holds.
   */
  offerBack: boolean;
}

/** What class of failure this is, or "unknown" for anything unclassified. */
export function classifyError(error: Error): FailureKind {
  if (error instanceof ApiParseError) {
    return "unreadable";
  }
  if (!(error instanceof ApiRequestError)) {
    return "unknown";
  }
  switch (error.status) {
    // No response at all: the fetch itself failed, so nothing answered.
    case 0:
      return "unreachable";
    case 400:
      return "invalid";
    case 403:
      return "forbidden";
    case 404:
      return "notFound";
    case 501:
      return "unsupported";
    case 502:
      return "upstream";
    case 504:
      return "timeout";
    default:
      // moxyd answered and failed on its own account; a 4xx we do not know
      // about is not worth a sentence of its own.
      return error.status >= 500 ? "internal" : "unknown";
  }
}

const EXPLANATIONS: Record<FailureKind, { title: string; body: string }> = {
  unreachable: {
    title: "moxy est injoignable",
    body:
      "Le service moxy n’a pas répondu. Vérifiez qu’il est démarré et que les " +
      "clusters sont joignables, puis réessayez.",
  },
  notFound: {
    title: "Objet introuvable",
    body:
      "Ce nœud ou cette machine n’existe plus dans le cluster : supprimé, " +
      "renommé, ou migré ailleurs ? La vue d’ensemble dit ce qui s’y trouve " +
      "encore.",
  },
  // A refusal is not an outage. The likely cause is named because there is
  // essentially only one: the ACL on /nodes overriding the one inherited from /.
  forbidden: {
    title: "Droits insuffisants sur ce nœud",
    body:
      "Proxmox a refusé la requête. Le token a besoin de Sys.Audit sur /nodes, " +
      "et un rôle posé sur /nodes remplace celui hérité de / au lieu de s’y " +
      "ajouter : un rôle ne portant que Sys.Modify efface Sys.Audit. " +
      "Voir « Privilèges PVE requis » dans le README.",
  },
  upstream: {
    title: "Cluster injoignable",
    body:
      "moxy répond, mais le cluster PVE ne répond pas. Le journal du serveur " +
      "dit pourquoi ; la vue d’ensemble continue d’afficher son dernier état " +
      "connu.",
  },
  timeout: {
    title: "Délai dépassé côté cluster",
    body:
      "Le cluster PVE n’a pas répondu dans le temps imparti. Il est peut-être " +
      "surchargé ; réessayez dans un instant.",
  },
  unsupported: {
    title: "Indisponible sans connexion au cluster",
    body:
      "moxy tourne sans connexion à ce cluster : cet écran a besoin d’un " +
      "cluster PVE configuré pour dire quoi que ce soit.",
  },
  invalid: {
    title: "Requête invalide",
    body:
      "moxy a refusé cette requête : un identifiant, une période ou une limite " +
      "n’est pas acceptable. Le détail technique ci-dessous nomme le paramètre " +
      "en cause.",
  },
  internal: {
    title: "Erreur interne de moxy",
    body:
      "Le service a échoué en traitant la requête. Le journal du serveur en " +
      "dit plus ; le détail technique ci-dessous donne le code.",
  },
  unreadable: {
    title: "Réponse inattendue",
    body:
      "La réponse reçue n’est pas celle de moxy : un proxy renvoie-t-il une " +
      "page HTML à sa place ? Le détail technique ci-dessous dit quelle " +
      "requête l’a reçue.",
  },
  unknown: {
    title: "Impossible de charger les données",
    body:
      "Une erreur inattendue s’est produite. Le détail technique ci-dessous en " +
      "dit plus ; réessayez.",
  },
};

/** The heading, the sentence and the way out, for one failure. */
export function explainError(error: Error): FailureExplanation {
  const kind = classifyError(error);
  const { title, body } = EXPLANATIONS[kind];
  return { kind, title, body, offerBack: kind === "notFound" };
}
