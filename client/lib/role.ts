import { Role } from "@/gen/auth/v1/auth_pb";

// roleLabel names a role as it is shown to the user.
export function roleLabel(role: Role): string {
  switch (role) {
    case Role.USER:
      return "user";
    case Role.ADMIN:
      return "admin";
    default:
      return "unknown";
  }
}
