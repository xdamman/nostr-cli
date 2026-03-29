import React from "react";
import { COLORS, SCENES } from "../styles";
import { useTerminalLines, type TerminalLine } from "../useTerminalLines";
import { TerminalRenderer } from "../TerminalRenderer";

const lines: TerminalLine[] = [
  {
    text: "nostr events --watch --kinds 4 --me --decrypt --jsonl",
    delay: 15,
    typewriter: true,
    charsPerFrame: 0.6,
    color: COLORS.command,
    prefix: "prompt",
  },
  {
    text: '{"from":"alice","message":"deploy the fix","protocol":"nip17","timestamp":"..."}',
    delay: 120,
    color: COLORS.textDim,
  },
  {
    text: '{"from":"bob","message":"PR merged ✓","protocol":"nip17","timestamp":"..."}',
    delay: 170,
    color: COLORS.textDim,
  },
  {
    text: '{"from":"alice","message":"done, shipped to prod","protocol":"nip17","timestamp":"..."}',
    delay: 220,
    color: COLORS.textDim,
  },
];

export const BotMode: React.FC = () => {
  const rendered = useTerminalLines(lines, SCENES.botMode.start);
  return <TerminalRenderer lines={rendered} />;
};
