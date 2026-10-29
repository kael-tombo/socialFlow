export const browserExtensions = [
  {
    slug: "youtube-localizer",
    name: "YouTube Localizer",
    title: "YouTube Localizer browser extension - SocialFlow.ai",
    description:
      "Translate video titles, descriptions and thumbnails in YouTube Studio. Generate new thumbnails with your own reference images.",
    headline: "Make your videos work in more languages.",
    repository: "https://github.com/getopenpost/youtube-localizer",
    chromeStoreUrl: "https://chromewebstore.google.com/detail/gblknofeccibdadogbplgcokneofgcdo",
    screenshot: "/assets/marketing/extensions/youtube-localizer-745a10444f66.webp",
    screenshotAlt:
      "YouTube Localizer’s Generate button beside a real video thumbnail in YouTube Studio",
    screenshotWidth: 950,
    screenshotHeight: 510,
    screenshotCaption: "Generate a thumbnail directly from YouTube Studio.",
    screenshots: [
      {
        src: "/assets/marketing/extensions/youtube-localizer-settings-light-4375ddc0c5ef.png",
        alt: "YouTube Localizer settings with text and image provider controls",
        caption: "Connect your translation and thumbnail providers.",
        width: 1280,
        height: 1061,
      },
      {
        src: "/assets/marketing/extensions/youtube-localizer-settings-advanced-6fac8c6ee1ff.png",
        alt: "YouTube Localizer advanced settings",
        caption: "Set image quality and a per-batch request limit.",
        width: 1280,
        height: 1287,
      },
    ],
    tone: "lilac",
    benefits: [
      {
        title: "Localize in Studio",
        text: "Choose your languages, select videos in Studio Content and click Localize. The extension checks for missing titles, descriptions and thumbnail text, then generates and saves them while preserving existing translations.",
      },
      {
        title: "Make your next thumbnail",
        text: "Keep optional face, brand and style references ready. Generate original thumbnails through Fal, then download them or use them as localization sources.",
      },
      {
        title: "Reuse paid work",
        text: "Batch translations across languages for each video. Prepare each source thumbnail background once, then render translated text locally. Saved results are reused when you retry unfinished work.",
      },
    ],
    setup: [
      "Install the extension and open Settings.",
      "Add your text-provider and Fal keys, then choose your target languages and components.",
      "With YouTube Studio in English, select videos on the Content page and click Localize.",
      "Wait for confirmed completion. The extension reports any items it could not save and keeps generated results for retry.",
    ],
    cost: "The extension is free. AI requests use your own provider keys and their pricing.",
    privacy:
      "YouTube Localizer works with your signed-in YouTube Studio session. It has no SocialFlow.ai account, backend, analytics or advertising tracking.",
    privacyUpdated: "12 October 2026",
    privacySections: [
      {
        title: "Data and providers",
        text: "Selected video titles, descriptions, thumbnails, reference images, language coverage and channel/video identifiers are used to find missing translations and save your results. When you request generation, relevant text and images go directly to your chosen text provider or Fal. API keys authenticate those requests. Your Google password and YouTube session credentials are not sent to AI providers. Providers handle submitted data under their own policies.",
      },
      {
        title: "Your data and deletion",
        text: "Settings, job history, references and generated images stay in local extension storage. API keys are session-only unless you choose to remember them. You can forget saved keys in Settings; uninstalling removes extension storage. Uninstalling does not delete downloaded files, provider-held data or translations already saved to YouTube. Exported backups omit provider keys and account bindings.",
      },
      {
        title: "Data use",
        text: "Data is used only for requested localization and thumbnail generation. It is not sold or used for advertising or credit decisions. Linked websites have their own privacy policies. Use the contact link below for questions; do not include keys or private video data in public issues.",
      },
    ],
  },
  {
    slug: "feed-limiter",
    name: "Feed Limiter",
    title: "Feed Limiter browser extension - SocialFlow.ai",
    description:
      "Pause feeds on X, YouTube, Instagram, TikTok, LinkedIn, Facebook, Reddit, Threads, Bluesky and Pinterest. Set site limits, a shared allowance and allowed hours.",
    headline: "Less scrolling. Keep the useful parts.",
    repository: "https://github.com/getopenpost/feed-limiter",
    chromeStoreUrl: "https://chromewebstore.google.com/detail/ckiajdbokbecejpammackpihhojjdbke",
    screenshot: "/assets/marketing/extensions/x-timeline-blocker-08db3f799836.webp",
    screenshotAlt: "A paused X home feed with posting and navigation still available",
    screenshotWidth: 1280,
    screenshotHeight: 800,
    screenshotCaption: "Pause the feed while keeping posting and navigation available.",
    screenshots: [
      {
        src: "/assets/marketing/extensions/feed-limiter-885e055990fe.png",
        alt: "Feed Limiter settings for YouTube feeds and distractions",
        caption: "Choose which feeds and distractions to pause for each site.",
        width: 1280,
        height: 800,
      },
      {
        src: "/assets/marketing/extensions/feed-limiter-shared-limits-4caa485f92d5.png",
        alt: "Shared allowance and allowed-hours settings",
        caption: "An example shared allowance and weekday browsing window.",
        width: 1280,
        height: 800,
      },
    ],
    tone: "blue",
    benefits: [
      {
        title: "Open feeds on purpose",
        text: "Selected feeds start paused. Choose Browse feed to start your allowance, then pause the session to keep unused time. Each site starts with five minutes per hour.",
      },
      {
        title: "Set limits that fit",
        text: "Choose hourly, two-hour or daily allowances, add a shared cap across sites, and set allowed days and hours. YouTube has separate controls for Home, Shorts, Subscriptions and player distractions.",
      },
      {
        title: "Keep the useful parts",
        text: "Direct posts, search, messages and creation tools stay available where the website keeps them separate from feeds. Controls apply to supported desktop websites, not native mobile apps.",
      },
    ],
    setup: [
      "Install the extension and open a supported social website.",
      "Choose Browse feed when you want to start a session.",
      "Open Settings to set site allowances, a shared cap and allowed hours.",
    ],
    cost: "Free. No extension account, subscription or API keys needed.",
    privacy:
      "Feed Limiter runs in your browser. It has no account, SocialFlow.ai backend, analytics or advertising tracking.",
    privacyUpdated: "12 October 2026",
    privacySections: [
      {
        title: "Data handled locally",
        text: "The extension reads supported page URLs and structure to identify and pause feeds. Settings, schedules, allowances and timers stay in your browser. It does not keep a browsing-history list or send page content, URLs or activity to SocialFlow.ai or another server.",
      },
      {
        title: "Your data and deletion",
        text: "Change preferences in Settings or uninstall the extension to remove its stored data. Browser and device backups are managed separately by your browser and operating system.",
      },
      {
        title: "Data use",
        text: "Data is used only to apply your feed limits. It is not sold or used for advertising or credit decisions. Linked websites have their own privacy policies. Use the contact link below for questions; do not include private browsing data in public issues.",
      },
    ],
  },
];
