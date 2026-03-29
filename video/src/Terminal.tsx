import React from "react";
import { useCurrentFrame, interpolate, Sequence } from "remotion";
import { TerminalWindow } from "./TerminalWindow";
import { SCENES, FPS, DURATION_FRAMES } from "./styles";
import { Login } from "./scenes/Login";
import { Post } from "./scenes/Post";
import { Follow } from "./scenes/Follow";
import { DM } from "./scenes/DM";
import { Accounts } from "./scenes/Accounts";
import { BotMode } from "./scenes/BotMode";
import { LongForm } from "./scenes/LongForm";

const FADE_FRAMES = 10; // ~0.33s fade

interface SceneConfig {
  id: string;
  start: number;
  end: number;
  component: React.FC;
}

const scenes: SceneConfig[] = [
  { id: "login", start: SCENES.login.start, end: SCENES.login.end, component: Login },
  { id: "post", start: SCENES.post.start, end: SCENES.post.end, component: Post },
  { id: "follow", start: SCENES.follow.start, end: SCENES.follow.end, component: Follow },
  { id: "dm", start: SCENES.dm.start, end: SCENES.dm.end, component: DM },
  { id: "accounts", start: SCENES.accounts.start, end: SCENES.accounts.end, component: Accounts },
  { id: "botMode", start: SCENES.botMode.start, end: SCENES.botMode.end, component: BotMode },
  { id: "longForm", start: SCENES.longForm.start, end: SCENES.longForm.end, component: LongForm },
];

const Scene: React.FC<{ scene: SceneConfig }> = ({ scene }) => {
  const frame = useCurrentFrame();
  const { start, end } = scene;
  const duration = end - start;

  const opacity = interpolate(
    frame,
    [start, start + FADE_FRAMES, end - FADE_FRAMES, end],
    [0, 1, 1, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" }
  );

  if (frame < start - 1 || frame > end + 1) return null;

  const Component = scene.component;

  return (
    <div
      style={{
        position: "absolute",
        top: 0,
        left: 0,
        width: "100%",
        height: "100%",
        opacity,
      }}
    >
      <Component />
    </div>
  );
};

export const Terminal: React.FC = () => {
  return (
    <TerminalWindow>
      <div style={{ position: "relative", width: "100%", height: "100%" }}>
        {scenes.map((scene) => (
          <Scene key={scene.id} scene={scene} />
        ))}
      </div>
    </TerminalWindow>
  );
};
