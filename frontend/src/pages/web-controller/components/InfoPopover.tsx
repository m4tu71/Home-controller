import { useId, useLayoutEffect, useRef, useState } from "react";
import type { FocusEvent, KeyboardEvent, MouseEvent, ReactNode } from "react";

import styles from "./InfoPopover.module.css";

type InfoPopoverProps = {
  subject: string;
  title: string;
  children: ReactNode;
  placement?: "inline" | "operation" | "embedded";
};

export function InfoPopover({ subject, title, children, placement = "inline" }: InfoPopoverProps) {
  const panelId = useId();
  const rootRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLElement>(null);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [pinnedOpen, setPinnedOpen] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const isOpen = pinnedOpen || (!dismissed && (hovered || focused));

  useLayoutEffect(() => {
    if (!isOpen) return;

    function positionPanel() {
      const trigger = rootRef.current?.querySelector("button");
      const panel = panelRef.current;
      if (!trigger || !panel) return;

      panel.style.maxHeight = "";
      const triggerRect = trigger.getBoundingClientRect();
      const panelRect = panel.getBoundingClientRect();
      const viewportWidth = document.documentElement.clientWidth || window.innerWidth;
      const viewportHeight = document.documentElement.clientHeight || window.innerHeight;
      const computed = window.getComputedStyle(panel);
      const edge = Number.parseFloat(computed.paddingLeft) || 12;
      const gap = Number.parseFloat(computed.getPropertyValue("--spacing-xs")) || 2;
      const panelWidth = panelRect.width || Math.min(352, viewportWidth - 2 * edge);
      const panelHeight = panelRect.height;
      const left = Math.max(edge, Math.min(triggerRect.right - panelWidth, viewportWidth - edge - panelWidth));
      const below = Math.max(0, viewportHeight - edge - triggerRect.bottom - gap);
      const above = Math.max(0, triggerRect.top - edge - gap);
      const placeAbove = panelHeight > below && above > below;
      const availableHeight = placeAbove ? above : below;
      const top = placeAbove
        ? triggerRect.top - gap - Math.min(panelHeight, availableHeight)
        : triggerRect.bottom + gap;

      panel.style.left = `${left}px`;
      panel.style.top = `${top}px`;
      panel.style.maxWidth = `${Math.max(0, viewportWidth - 2 * edge)}px`;
      panel.style.maxHeight = `${availableHeight}px`;
    }

    positionPanel();
    window.addEventListener("resize", positionPanel);
    window.addEventListener("scroll", positionPanel, true);
    return () => {
      window.removeEventListener("resize", positionPanel);
      window.removeEventListener("scroll", positionPanel, true);
    };
  }, [isOpen]);

  function handleBlur(event: FocusEvent<HTMLDivElement>) {
    if (rootRef.current?.contains(event.relatedTarget as Node | null)) return;
    setFocused(false);
    setPinnedOpen(false);
    if (!hovered) setDismissed(false);
  }

  function handleClick(event: MouseEvent<HTMLButtonElement>) {
    event.stopPropagation();
    if (pinnedOpen) {
      setPinnedOpen(false);
      setDismissed(true);
      return;
    }
    setPinnedOpen(true);
    setDismissed(false);
  }

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== "Escape" || !isOpen) return;
    event.preventDefault();
    event.stopPropagation();
    setPinnedOpen(false);
    setDismissed(true);
    rootRef.current?.querySelector("button")?.focus();
  }

  return (
    <div
      ref={rootRef}
      className={`${styles.anchor} ${placement === "operation" ? styles.operationAnchor : placement === "embedded" ? styles.embeddedAnchor : ""} ${isOpen ? styles.anchorOpen : ""}`}
      onMouseEnter={() => {
        setHovered(true);
        setDismissed(false);
      }}
      onMouseLeave={() => {
        setHovered(false);
        if (!focused) setDismissed(false);
      }}
      onFocus={() => {
        setFocused(true);
        setDismissed(false);
      }}
      onBlur={handleBlur}
      onKeyDown={handleKeyDown}
    >
      <button
        type="button"
        className={styles.button}
        aria-label={`${subject}の情報`}
        aria-expanded={isOpen}
        aria-controls={panelId}
        onClick={handleClick}
      >
        情報
      </button>
      <section
        ref={panelRef}
        id={panelId}
        className={styles.panel}
        role="region"
        aria-labelledby={`${panelId}-title`}
        hidden={!isOpen}
      >
        <h4 id={`${panelId}-title`} className={styles.title}>
          {title}
        </h4>
        {children}
      </section>
    </div>
  );
}
