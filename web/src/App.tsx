import { Route, Routes } from 'react-router-dom';
import Layout from './components/Layout';
import AgentDetail from './pages/AgentDetail';
import Automations from './pages/Automations';
import Chat from './pages/Chat';
import Evals from './pages/Evals';
import Overview from './pages/Overview';
import Projects from './pages/Projects';
import Replays from './pages/Replays';
import Room from './pages/Room';
import Studio from './pages/Studio';
import TaskDetail from './pages/TaskDetail';
import Tasks from './pages/Tasks';
import Usage from './pages/Usage';

export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Overview />} />
        <Route path="tasks" element={<Tasks />} />
        <Route path="tasks/:id" element={<TaskDetail />} />
        <Route path="agents/:id" element={<AgentDetail />} />
        <Route path="projects" element={<Projects />} />
        <Route path="automations" element={<Automations />} />
        <Route path="studio" element={<Studio />} />
        <Route path="replays" element={<Replays />} />
        <Route path="chat" element={<Chat />} />
        <Route path="room" element={<Room />} />
        <Route path="evals" element={<Evals />} />
        <Route path="usage" element={<Usage />} />
        <Route path="*" element={<div className="text-sm text-muted">404 · 页面不存在</div>} />
      </Route>
    </Routes>
  );
}
