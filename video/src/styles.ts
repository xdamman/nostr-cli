export const COLORS = {
  bg: "#0d1117",
  windowChrome: "#1c2128",
  windowBorder: "#30363d",
  text: "#e6edf3",
  textDim: "#7d8590",
  prompt: "#3fb950",
  command: "#79c0ff",
  success: "#3fb950",
  warning: "#d29922",
  label: "#58a6ff",
  npub: "#7d8590",
  json: "#e6edf3",
  jsonKey: "#ff7b72",
  jsonString: "#a5d6ff",
  red: "#ff7b72",
  yellow: "#d29922",
  green: "#3fb950",
  cyan: "#58a6ff",
} as const;

export const FONT = {
  family: "'JetBrains Mono', 'SF Mono', 'Fira Code', 'Cascadia Code', Consolas, 'Courier New', monospace",
  size: 18,
  lineHeight: 1.6,
} as const;

export const FPS = 30;
export const DURATION_FRAMES = 1800; // ~60 seconds

// Scene timing in frames
export const SCENES = {
  login:    { start: 0,    end: 240 },   // 0-8s
  post:     { start: 240,  end: 450 },   // 8-15s
  follow:   { start: 450,  end: 690 },   // 15-23s
  dm:       { start: 690,  end: 960 },   // 23-32s
  accounts: { start: 960,  end: 1170 },  // 32-39s
  botMode:  { start: 1170, end: 1440 },  // 39-48s
  longForm: { start: 1440, end: 1800 },  // 48-60s
} as const;
