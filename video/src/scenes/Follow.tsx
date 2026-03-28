import React from "react";
import { COLORS, SCENES } from "../styles";
import { useTerminalLines, type TerminalLine } from "../useTerminalLines";
import { TerminalRenderer } from "../TerminalRenderer";

const lines: TerminalLine[] = [
  {
    text: "nostr follow fiatjaf@nostr.com",
    delay: 15,
    typewriter: true,
    color: COLORS.command,
    prefix: "prompt",
  },
  { text: "✓ Following fiatjaf (npub1sg6...e2uf)", delay: 65, color: COLORS.success },
  { text: "", delay: 80 },
  {
    text: "nostr fiatjaf@nostr.com",
    delay: 100,
    typewriter: true,
    color: COLORS.command,
    prefix: "prompt",
  },
  { text: "Name:  fiatjaf", delay: 145, color: COLORS.text, indent: 1 },
  { text: "NIP-05: fiatjaf@nostr.com ✓", delay: 158, color: COLORS.text, indent: 1 },
  { text: "About: creating nostr and other things", delay: 171, color: COLORS.textDim, indent: 1 },
];

export const Follow: React.FC = () => {
  const rendered = useTerminalLines(lines, SCENES.follow.start);
  return <TerminalRenderer lines={rendered} />;
};
