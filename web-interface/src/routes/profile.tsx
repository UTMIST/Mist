import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { SquarePen } from 'lucide-react'
import { getUser } from '#/util.ts'

export const Route = createFileRoute('/profile')({
  component: ProfilePage,
})

function ProfileField({
                        label,
                        value,
                        type = 'text',
                        disabled,
                        onChange,
                      }: {
  label: string
  value: string
  type?: string
  disabled: boolean
  onChange: (val: string) => void
}) {
  return (
    <div>
      <label className="block text-base font-medium mb-1">{label}</label>
      <input
        type={type}
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        className="w-full border border-gray-300 rounded-lg px-4 py-2 text-sm disabled:bg-white disabled:text-gray-700"
      />
    </div>
  )
}

function ProfilePage() {
  const user = getUser()
  const [editing, setEditing] = useState(false)

  const [name, setName] = useState('John Doe')
  const [role, setRole] = useState('Software Developer')
  const [email, setEmail] = useState('john@utmist.ca')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')

  function handleSave() {
    // TODO: call API to update profile
    console.log('Save profile:', { name, role, email, password })
    setEditing(false)
  }

  return (
    <div className="w-fit mx-auto py-8 px-8">
      <div className="flex gap-16 items-start">
        {/* Left: form fields */}
        <div className="flex-1 min-w-80 flex flex-col gap-5">
          <ProfileField
            label="Name"
            value={name}
            disabled={!editing}
            onChange={setName}
          />
          <ProfileField
            label="Role"
            value={role}
            disabled={!editing}
            onChange={setRole}
          />
          <ProfileField
            label="Email"
            value={email}
            type="email"
            disabled={!editing}
            onChange={setEmail}
          />
          <ProfileField
            label="Password"
            value={password}
            type="password"
            disabled={!editing}
            onChange={setPassword}
          />
          <ProfileField
            label="Confirm Password"
            value={confirmPassword}
            type="password"
            disabled={!editing}
            onChange={setConfirmPassword}
          />

          <div className="flex gap-3 mt-2">
            <button
              onClick={handleSave}
              className="px-6 py-2 text-sm font-semibold rounded-lg text-white hover:opacity-90"
              style={{ backgroundColor: '#3C5BDB' }}
            >
              Save
            </button>
            <button
              onClick={() => setEditing(!editing)}
              className="px-6 py-2 text-sm font-semibold rounded-lg border hover:opacity-80"
              style={{ borderColor: '#3C5BDB', color: '#3C5BDB' }}
            >
              Edit
            </button>
          </div>
        </div>

        {/* Right: profile picture */}
        <div className="flex flex-col items-center gap-3 pt-6">
          <div className="relative">
            <img
              src={user.profilePicture}
              alt="Profile Picture"
              className="w-48 h-48 rounded-full object-cover border-2 border-gray-200"
            />
            <button
              onClick={() => {
                // TODO: open file picker to upload new avatar
                console.log('Edit profile picture')
              }}
              className="absolute bottom-3 left-3 flex items-center gap-1.5 px-4 py-1.5 text-sm font-semibold rounded text-white hover:opacity-90"
              style={{ backgroundColor: '#3C5BDB' }}
            >
              Edit <SquarePen size={16} />
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
