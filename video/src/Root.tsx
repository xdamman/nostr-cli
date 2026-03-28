import React from "react";
import { Composition } from "remotion";
import { Terminal } from "./Terminal";
import { FPS, DURATION_FRAMES } from "./styles";

export const Root: React.FC = () => {
  return (
    <Composition
      id="Terminal"
      component={Terminal}
      durationInFrames={DURATION_FRAMES}
      fps={FPS}
      width={1280}
      height={720}
    />
  );
};
