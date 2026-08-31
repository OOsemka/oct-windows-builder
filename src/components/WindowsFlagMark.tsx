import React, { FC } from 'react';

type Props = {
  muted?: boolean;
};

/** Original four-pane mark (geometric UI chrome). Not Microsoft’s Windows logo. */
const WindowsFlagMark: FC<Props> = ({ muted }) => (
  <span className={muted ? 'wb-win-flag wb-win-flag-muted' : 'wb-win-flag'} aria-hidden>
    <span />
    <span />
    <span />
    <span />
  </span>
);

export default WindowsFlagMark;
