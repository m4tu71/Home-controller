import styles from "../pages/web-controller/WebController.module.css";

export function Step9BFixtureNotice() {
  return (
    <div className={styles.warningText} role="alert">
      Step 9B専用fixture：表示データはブラウザ内のmockです。`/api/controller/*` は外部へ送信されず、すべての書き込み・送信操作は遮断されます。
    </div>
  );
}
