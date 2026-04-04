import { createFileRoute } from '@tanstack/react-router'
import { SquarePen } from 'lucide-react'
import { getUser } from '#/util.ts'
import { useImmer } from 'use-immer'
import {Button} from "#/components/Buttons.tsx";

export const Route = createFileRoute('/profile')({
  component: ProfilePage,
  loader: getUser,
})

function ProfileField({
  label,
  value,
  type = 'text',
  onChange,
}: {
  label: string
  value: string
  type?: string
  onChange: (val: string) => void
}) {
  return (
    <div>
      <label className="block text-base font-medium mb-1">{label}</label>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full border border-gray-300 rounded-lg px-4 py-2 text-sm disabled:bg-white disabled:text-gray-700"
      />
    </div>
  )
}

function ProfilePage() {
  const loaderData = Route.useLoaderData()
  const [user, setUser] = useImmer({
    ...loaderData,
    password: '',
    confirmPassword: '',
  })

  function handleSave() {
    // TODO: call API to update profile
    console.log('Save profile:', {
      username: user.username,
      role: user.role,
      email: user.email,
      password: user.password,
    })
  }

  function handleCancel() {
    const confirmCancel = confirm("Are you sure you want to cancel?");

    if (confirmCancel) {
      setUser({
        ...loaderData,
        password: '',
        confirmPassword: '',
      });
    }
  }

  return (
    <div className="w-fit mx-auto py-8 px-8">
      <div className="flex gap-16 items-start">
        {/* Left: form fields */}
        <div className="flex-1 min-w-80 flex flex-col gap-5">
          <ProfileField
            label="Username"
            value={user.username}
            onChange={(username) =>
              setUser((draft) => {
                draft.username = username
              })
            }
          />
          <ProfileField
            label="Role"
            value={user.role}
            onChange={(role) =>
              setUser((draft) => {
                draft.role = role
              })
            }
          />
          <ProfileField
            label="Email"
            value={user.email}
            type="email"
            onChange={(email) =>
              setUser((draft) => {
                draft.email = email
              })
            }
          />
          <ProfileField
            label="Password"
            value={user.password}
            type="password"
            onChange={(password) =>
              setUser((draft) => {
                draft.password = password
              })
            }
          />
          <ProfileField
            label="Confirm Password"
            value={user.confirmPassword}
            type="password"
            onChange={(confirmPassword) =>
              setUser((draft) => {
                draft.confirmPassword = confirmPassword
              })
            }
          />

          <div className="flex gap-3 mt-2">
            <Button
              onClick={handleSave}
              variant="normal"
              fontSize="base"
            >Save</Button>
            <Button
              onClick={handleCancel}
              variant="danger"
              fontSize="base">
              Cancel
            </Button>
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
            <div className="absolute bottom-3 left-3">
              <Button
                onClick={() => {
                  // TODO: open file picker to upload new avatar
                  console.log('Edit profile picture')
                }}
                variant="normal"
                fontSize="base"
              >
                Edit <SquarePen size={16} />
              </Button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
