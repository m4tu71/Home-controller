import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './styles/global.css'
import App from './App.tsx'

const rootElement = document.getElementById('root')!
const fixtureRequested = new URLSearchParams(window.location.search).get('fixture') === 'step9b-stateful-empty'
const sendFixtureRequested = new URLSearchParams(window.location.search).get('fixture') === 'step17m-send-result'

async function startApp() {
  if (sendFixtureRequested) {
    if (import.meta.env.DEV) {
      try {
        const [fixture, notice] = await Promise.all([
          import('./devFixtures/step17mSendResultFixture'),
          import('./devFixtures/Step17MSendResultFixtureNotice'),
        ])
        const requestedDelivery = new URLSearchParams(window.location.search).get('delivery')
        const delivery = requestedDelivery === 'not_sent' || requestedDelivery === 'partially_sent' || requestedDelivery === 'unknown'
          ? requestedDelivery
          : 'sent'
        fixture.installStep17MSendResultFixture(delivery)
        createRoot(rootElement).render(
          <StrictMode>
            <>
              <notice.Step17MSendResultFixtureNotice />
              <App />
            </>
          </StrictMode>,
        )
      } catch {
        createRoot(rootElement).render(<p role="alert">Step 17M fixture failed to start. The application was not started.</p>)
      }
    } else {
      createRoot(rootElement).render(<p role="alert">Step 17M fixture is available only in the Vite development server. The application was not started.</p>)
    }
    return
  }
  if (fixtureRequested) {
    if (import.meta.env.DEV) {
      try {
        const [fixture, notice] = await Promise.all([
          import('./devFixtures/step9bStatefulEmptyFixture'),
          import('./devFixtures/Step9BFixtureNotice'),
        ])
        fixture.installStep9BStatefulEmptyFixture()
        createRoot(rootElement).render(
          <StrictMode>
            <>
              <notice.Step9BFixtureNotice />
              <App />
            </>
          </StrictMode>,
        )
        return
      } catch {
        createRoot(rootElement).render(
          <p role="alert">Step 9B fixture failed to start. The application was not started.</p>,
        )
        return
      }
    } else {
      createRoot(rootElement).render(
        <p role="alert">Step 9B fixture is available only in the Vite development server. The application was not started.</p>,
      )
      return
    }
  }

  createRoot(rootElement).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
}

void startApp()
