// SectionAbout explains one section of the app in three parts: what it is
// responsible for, what it does, and how that helps the person using it.
export interface SectionAbout {
  // id keys the remembered open or closed state, so it must be unique and stable.
  id: string;
  role: string;
  does: string;
  helps: string;
}

// The explainer copy of every section, in one file so it is reviewed in one place.
export const SECTIONS = {
  jobs: {
    id: "jobs",
    role: "Jobs keeps every application you have in play, from the first idea to the final answer.",
    does: "Each job is a card on a board that moves through stages such as saved, applied, interviewing and offer. It keeps a timeline of what happened, reminds you when a follow-up is due, and can import a spreadsheet of jobs you already track.",
    helps: "You never lose track of where an application stands or forget to follow up, and you can see the whole search at a glance instead of hunting through email and notes.",
  },
  contacts: {
    id: "contacts",
    role: "Contacts is your network: the recruiters, referrers and colleagues who matter to your search.",
    does: "It stores each person with their company, where the conversation stands and a timeline of every message and note. It tells you when it is time to reach out again, and links people to the jobs they relate to.",
    helps: "Warm introductions are the best way into a role. Keeping the relationship history in one place means you reach out at the right time with the right context.",
  },
  drafts: {
    id: "drafts",
    role: "Drafts is the approval queue for everything Shogun writes for you: cover letters, outreach messages and posts.",
    does: "Shogun writes drafts in your voice using the AI provider you set up, keeps every version, and holds each one here. Nothing is sent until you approve that exact version, and email is only ever sent after your approval. LinkedIn and X posts are copy-and-paste only.",
    helps: "You save the time of staring at a blank page while keeping the last word. Because you approve the exact text, nothing goes out under your name that you have not read.",
  },
  learning: {
    id: "learning",
    role: "Learning is your log of what you are studying and building: courses, books, projects and skills.",
    does: "You add items, move them from planned to in progress to done, and log what you did on them. From your activity, Shogun can draft a post about what you learned for you to review.",
    helps: "A record of steady progress is proof of growth you can point to in interviews, and turning it into posts keeps you visible without extra effort.",
  },
  profile: {
    id: "profile",
    role: "Profile keeps your resume and LinkedIn up to date with the work you actually do.",
    does: "Once you connect GitHub and an AI provider, Shogun reads your repositories and contributions and suggests specific edits to your headline, about section, experience, skills and projects, each with the evidence behind it. You accept or dismiss each one.",
    helps: "Your public profile stops lagging behind your real work, and each suggestion shows why it was made, so you stay in control of what you claim.",
  },
  discovery: {
    id: "discovery",
    role: "Discovery finds new roles for you from the sources you choose.",
    does: "You add the sources to watch. Shogun reads them on a schedule, scores each posting against what you are looking for using your AI provider, and lists the best matches. Saving a posting copies it to your job board.",
    helps: "You spend your time on the postings most likely to fit instead of scrolling through everything, and nothing good slips past between visits.",
  },
  insights: {
    id: "insights",
    role: "Insights shows how your search is going, in numbers.",
    does: "It counts the jobs in each stage and the outreach you have sent and how it is landing, by day, from the events the rest of Shogun records. Figures update after the nightly rollup.",
    helps: "You can see what is working, such as which stage stalls or which kind of message gets replies, and change your approach on evidence rather than a hunch.",
  },
  notifications: {
    id: "notifications",
    role: "Notifications is where Shogun tells you what needs your attention.",
    does: "It lists reminders and events such as follow-ups that are due, drafts waiting for approval, new mail Shogun classified as job-related, and budget warnings. A daily digest summarises the day.",
    helps: "Important things reach you in one place, so a recruiter's reply or an expiring follow-up does not get buried.",
  },
  provider: {
    id: "provider",
    role: "AI provider is where you give Shogun the AI it works with, so every call is made with your own provider and your own key.",
    does: "You enter the address of an Anthropic-compatible provider, the model, and your API key. Shogun stores the key encrypted and never shows it again. It uses that provider for your drafts, for sorting your job mail, for profile suggestions and for scoring discovered jobs. You can set prices for a model Shogun has no price for, and a monthly token limit. Test connection makes one tiny call to check it works.",
    helps: "Nobody else's usage or bill is mixed with yours, and you choose the model and the cost. Until you add a provider, the AI features stay off and everything else keeps working.",
  },
  settings: {
    id: "settings",
    role: "Settings holds the keys and connections that belong to you alone.",
    does: "Here you add your AI provider, connect the accounts Shogun works with, such as Gmail, see and limit what your AI calls cost, and choose how Shogun reaches you. Keys and tokens are stored encrypted and used only for your account.",
    helps: "You decide what Shogun can reach and spend. Features that need a connection stay off until you set it up, so nothing happens that you did not choose.",
  },
  mail: {
    id: "mail",
    role: "Mail is the connection between Shogun and your Gmail account.",
    does: "After an admin approves your account for Gmail, you connect it with Google. Shogun then reads job-related mail, classifies it, links it to your jobs and contacts, and sends the drafts you approve. It never sends anything you have not approved.",
    helps: "Replies and rejections update your board for you, and approved messages go out from your own address without copying and pasting.",
  },
  github: {
    id: "github",
    role: "GitHub is the connection between Shogun and your GitHub account.",
    does: "You connect it with GitHub and Shogun asks for no permissions beyond your public profile, so it can read only what anyone can see and can never change anything. It then looks at your public repositories and merged work to suggest resume and LinkedIn edits. Your token is stored encrypted, used only for your account, and deleted when you disconnect.",
    helps: "Your resume and LinkedIn keep up with what you have actually built, without you remembering to update them. Until you connect, Profile has nothing to sync and everything else keeps working.",
  },
  account: {
    id: "account",
    role: "Account is where you control how Shogun treats you as a person: your time zone and your sessions.",
    does: "You choose the time zone your days follow, so reminders, the daily digest and daily summaries arrive when your day starts and ends. You can also sign out on every device at once.",
    helps: "Reminders land at the right hour wherever you live or travel, and a lost phone or a shared computer never keeps access to your Shogun.",
  },
  spend: {
    id: "spend",
    role: "Spend is the meter for the AI calls made on your behalf.",
    does: "Every call to your AI provider is recorded with its cost, grouped by service or feature. You can set daily and monthly budgets: a hard budget stops new calls at its limit, a soft one only warns you.",
    helps: "You always know what the AI costs you, and a limit stops a runaway bill before it happens. Models without a known price are counted in tokens.",
  },
  alerts: {
    id: "alerts",
    role: "Alerts controls how and when Shogun notifies you.",
    does: "You can switch in-app notifications and the daily digest on or off, and set quiet hours when nothing is shown.",
    helps: "You get the nudges that keep your search moving without being interrupted when you do not want to be.",
  },
  users: {
    id: "users",
    role: "Users is the admin view of who has signed up to Shogun.",
    does: "Admins see each account with its role and status, can disable or re-enable an account, and can approve an account to connect Gmail while the Google app is unverified. Admins see accounts only, never anyone's jobs, contacts, mail or drafts.",
    helps: "You keep the service safe and within Google's limits for Gmail without ever reading anyone's private data.",
  },
} as const satisfies Record<string, SectionAbout>;
